package compiler

import (
	"bytes"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/PiMaker/mscr/yard"
)

const CalcTypeRegexLiteral = `^(?:0x)?\d+$`
const CalcTypeRegexMath = `^(?:\=\=|\!\=|\<\=|\>\=|\<\<|\>\>|\+|\-|\<|\>|\*|\/|\%|\(|\)|\s|,|\~|[a-zA-Z0-9_])*$`

var calcTypeRegexLiteralRegexp = regexp.MustCompile(CalcTypeRegexLiteral)
var calcTypeRegexMathRegexp = regexp.MustCompile(CalcTypeRegexMath)

func resolveCalc(calc string, scope string, state *asmTransformState) []*asmCmd {
	// Remove square brackets, they are just indicators that this is a calc value string
	calc = strings.Replace(calc, "[", "", -1)
	calc = strings.Replace(calc, "]", "", -1)
	calc = strings.Trim(calc, " \t")

	// Match type of expression using regex
	if calcTypeRegexLiteralRegexp.MatchString(calc) {

		return setRegToLiteralFromString(calc, "F") // F is calc out register

	} else if calcTypeRegexMathRegexp.MatchString(calc) {

		// Math/Function parsing
		ensureShuntingYardParser()
		shunted := callShuntingYardParser(calc)
		output := make([]*asmCmd, 0)

		// Function call temp vars
		var funcFunct string
		funcStackOffset := 0
		var funcFunargLast int

		for _, token := range shunted {
			switch token.tokenType {
			case "FUNCT":
				funcFunct = token.value
			case "FUNARG":
				funcFunarg, _ := strconv.Atoi(token.value)
				// Offset pop count because called function will pop values from stack for us
				funcStackOffset -= funcFunarg
				funcFunargLast = funcFunarg

			case "SYS":
				switch token.value {
				case "INVOKE":
					// Call function and push return value to stack
					output = append(output, callCalcFunc(funcFunct, funcFunargLast, state)...)
				}

			case "OPRND":
				// First, put operand in F
				if calcTypeRegexLiteralRegexp.MatchString(token.value) {
					output = append(output, setRegToLiteralFromString(token.value, "F")...)
				} else {
					// Assume variable or global
					cmd := &asmCmd{
						ins: "MOV",
						params: []*asmParam{
							&asmParam{
								asmParamType: asmParamTypeVarRead,
								value:        token.value,
							},
							&asmParam{
								asmParamType: asmParamTypeRaw,
								value:        "F",
							},
						},
						comment: " CALC: var " + token.value,
					}

					// Take care of globals and string addresses
					cmd.fixGlobalAndStringParamTypes(state)

					output = append(output, cmd)
				}

				// Then, push F onto stack
				output = append(output, &asmCmd{
					ins: "PUSH",
					params: []*asmParam{
						&asmParam{
							asmParamType: asmParamTypeRaw,
							value:        "F",
						},
					},
					comment: " CALC: push operand",
				})

			case "OPER":
				switch token.value {
				case "+", "*", "-", "&", "|", "^", "==", "<", ">", "<=", ">=", "!=":
					// Pop twice then calculate then push again
					output = append(output, &asmCmd{
						ins: "POP",
						params: []*asmParam{
							&asmParam{
								asmParamType: asmParamTypeRaw,
								value:        "E",
							},
						},
					})
					output = append(output, &asmCmd{
						ins: "POP",
						params: []*asmParam{
							&asmParam{
								asmParamType: asmParamTypeRaw,
								value:        "F",
							},
						},
					})

					aluIns := symbolToALUFuncName(token.value)
					output = append(output, &asmCmd{
						ins: aluIns,
						params: []*asmParam{
							&asmParam{
								asmParamType: asmParamTypeRaw,
								value:        "F", // Input 1
							},
							&asmParam{
								asmParamType: asmParamTypeRaw,
								value:        "F", // Output
							},
							&asmParam{
								asmParamType: asmParamTypeRaw,
								value:        "E", // Input 2
							},
						},
						comment: " CALC: operator " + aluIns,
					})

					output = append(output, &asmCmd{
						ins: "PUSH",
						params: []*asmParam{
							&asmParam{
								asmParamType: asmParamTypeRaw,
								value:        "F",
							},
						},
					})

				case ".-", ".~", "~":
					output = append(output, &asmCmd{
						ins: "POP",
						params: []*asmParam{
							&asmParam{
								asmParamType: asmParamTypeRaw,
								value:        "F",
							},
						},
					})

					aluIns := "COM"
					if token.value == ".-" {
						aluIns = "NEG"
					}

					output = append(output, &asmCmd{
						ins: aluIns,
						params: []*asmParam{
							&asmParam{
								asmParamType: asmParamTypeRaw,
								value:        "F",
							},
							&asmParam{
								asmParamType: asmParamTypeRaw,
								value:        "F",
							},
						},
					})

					output = append(output, &asmCmd{
						ins: "PUSH",
						params: []*asmParam{
							&asmParam{
								asmParamType: asmParamTypeRaw,
								value:        "F",
							},
						},
					})

				default:
					log.Fatalln("ERROR: Unsupported tokenType returned from shunting yard parser. This calc feature is probably not implemented yet. (" + token.tokenType + " = " + token.value + ")")
				}
			}
		}

		output = append(output, &asmCmd{
			ins: "POP",
			params: []*asmParam{
				&asmParam{
					asmParamType: asmParamTypeRaw,
					value:        "F",
				},
			},
		})

		// Validate result to preserve stack correctness
		stackValue := 0
		for _, c := range output {
			if c.ins == "PUSH" {
				stackValue++
			} else if c.ins == "POP" {
				stackValue--
			}
		}

		// Function calls modify the stack without push-es or pop-s
		stackValue += funcStackOffset

		if stackValue != 0 {
			spew.Dump(shunted)
			log.Fatalln("ERROR: Calc instruction produced invalid stack. This is *probably* a compiler bug. (Stack value: " + strconv.Itoa(stackValue) + ")")
		}

		// Set scope of "parent" (calc instruction) on all generated "child" instructions
		for _, a := range output {
			a.scope = scope
		}

		// Shortcut: If last two instructions are "PUSH F", "POP F", leaving them out will still put result in F
		if len(output) > 1 && output[len(output)-2].ins == "PUSH" && len(output[len(output)-2].params) == 1 &&
			output[len(output)-2].params[0].asmParamType == asmParamTypeRaw && output[len(output)-2].params[0].value == "F" {

			return output[0 : len(output)-2]
		}

		return output

	} else {
		log.Fatalln("ERROR: Unsupported calc string: " + calc)
		return nil
	}
}

func symbolToALUFuncName(oper string) string {
	switch oper {
	case "*":
		return "MUL"
	case "+":
		return "ADD"
	case "-":
		return "SUB"
	case "^":
		return "XOR"
	case "&":
		return "AND"
	case "|":
		return "OR"
	case "==":
		return "EQ"
	case "!=":
		return "NEQ"
	case ">":
		return "GT"
	case "<":
		return "LT"
	case "<=":
		return "LTOE"
	case ">=":
		return "GTOE"
	default:
		log.Fatalln("ERROR: Unsupported operator in calc instruction: " + oper)
		return ""
	}
}

func setRegToLiteralFromString(calc, reg string) []*asmCmd {
	var calcValue int64
	if strings.Index(calc, "0x") == 0 {
		// Error ignored, format is validated at this point
		calcValue, _ = strconv.ParseInt(calc[2:], 16, 16)
	} else {
		calcValue, _ = strconv.ParseInt(calc, 10, 16)
	}

	return []*asmCmd{
		&asmCmd{
			ins: "SETREG",
			params: []*asmParam{
				&asmParam{
					asmParamType: asmParamTypeRaw,
					value:        reg,
				},
				&asmParam{
					asmParamType: asmParamTypeRaw,
					value:        "0x" + strconv.FormatInt(calcValue, 16),
				},
			},
			comment: " CALC: literal " + calc,
		},
	}
}

func callCalcFunc(funcName string, paramCount int, state *asmTransformState) []*asmCmd {
	retval := make([]*asmCmd, 0)

	// Scope handling should still work in calc context, recursive resolving is really quite something huh?
	retval = append(retval, &asmCmd{
		ins: "__FLUSHSCOPE",
	})

	retval = append(retval, &asmCmd{
		ins: "__CLEARSCOPE",
	})

	fLabel := getFuncLabelSpecific(funcName, paramCount)
	function := ""
	for _, varFunc := range state.functionTableVar {
		if varFunc == fLabel {
			function = varFunc
			break
		}
	}

	if function == "" {
		for _, voidFunc := range state.functionTableVoid {
			if voidFunc == fLabel {
				log.Fatalf("ERROR: Tried calling a void function in a calc context: Function '%s' with %d parameters\n", funcName, paramCount)
			}
		}

		if function == "" {
			log.Printf("WARNING: Cannot find function to call (calc): Function '%s' with %d parameters (Assuming extern function)\n", funcName, paramCount)
			function = fLabel
		}
	}

	retval = append(retval, &asmCmd{
		ins: "CALL",
		params: []*asmParam{
			&asmParam{
				asmParamType: asmParamTypeRaw,
				value:        "." + function,
			},
		},
	})

	// Push returned value to stack for further calculations
	retval = append(retval, &asmCmd{
		ins: "PUSH",
		params: []*asmParam{
			&asmParam{
				asmParamType: asmParamTypeRaw,
				value:        "A",
			},
		},
	})

	return append(retval, &asmCmd{
		ins: "__CLEARSCOPE",
	})
}

var parserExtracted = false
var dijkstraPath = ""

func ensureShuntingYardParser() {
	if parserExtracted && dijkstraPath != "" {
		return
	}

	// Extract parser from go-bindata
	dijkstraPath = os.TempDir() + string(os.PathSeparator)
	err := yard.RestoreAssets(dijkstraPath, "dijkstra-shunting-yard")

	if err != nil {
		log.Fatalln("ERROR: Could not extract dijkstra parser: " + err.Error())
	}

	dijkstraPath += "dijkstra-shunting-yard" + string(os.PathSeparator)
	parserExtracted = true
}

func callShuntingYardParser(calc string) []*YardToken {
	cmd := exec.Command(dijkstraPath + "shunt.sh")

	var out bytes.Buffer
	in := bytes.NewBufferString(calc + " ") // Space is needed, trust me

	cmd.Stdin = in
	cmd.Stdout = &out

	err := cmd.Run()

	output := string(out.Bytes())

	if err != nil {
		log.Println("Debug log of shunt.sh:")
		fmt.Println(output)

		log.Fatalln("ERROR: While executing shunting yard parser: " + err.Error())
	}

	// Check for error output
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, shuntSplit+"error") {
			log.Println("ERROR: Shunting yard parser encountered an error on a calc expression:")
			log.Println("Calc: " + calc)
			log.Fatalln("Error: " + line)
		}
	}

	return parseIntoYardTokens(string(out.Bytes()))
}
