package autotest

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/PiMaker/MCPC-Software/interpreter"
	"github.com/PiMaker/MCPC-Software/mscr"
	"github.com/logrusorgru/aurora"
)

var regexpRegister = regexp.MustCompile(`(?m)^;autotest.*reg=(\S+).*?;`)
var regexpExpected = regexp.MustCompile(`(?m)^;autotest.*val=(\S+).*?;`)

// RunAutotests calls all autotests in a directory in sequence
func RunAutotests(dir string, libraries []string, optimizeDisable bool) {
	log.Println("Starting autotests in directory: " + dir)

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		log.Fatalln(err)
	}

	counter := -1
	failedTotal := 0
	perfTrace := 0
	for _, f := range files {
		if !f.IsDir() {
			counter++

			stateOut := aurora.Blue("UNKN").String()
			output := fmt.Sprintf("Test %d: ", counter)

			if strings.HasSuffix(f.Name(), ".mscr") {
				output = fmt.Sprintf("%s%s (MSCR", output, f.Name())

				tmpFile := path.Join(os.TempDir(), "mcpc_autotest.ma")
				success, state, mscrOut := callMscr(path.Join(dir, f.Name()), tmpFile, optimizeDisable)
				stateOut = state

				if !success {
					if mscrOut != "" {
						log.Printf(aurora.Bold("Test %d: vvvvv MSCR failed to compile, output log below this line vvvvv\r\n").String(), counter)
						fmt.Println(mscrOut)
						failedTotal++
					}
					output = fmt.Sprintf("%s, MSCR failure", output)
					printTestResult(stateOut, output)
					continue
				}

				state, testOut, inses := performAutotest(tmpFile, counter, libraries)
				stateOut = state
				output = fmt.Sprintf("%s, %s", output, testOut)

				if inses > 0 {
					perfTrace += inses
				}

				if state == aurora.Red("FAIL").String() { // WTF
					failedTotal++

					assemblerBackup := path.Join(os.TempDir(), f.Name()+"_autotest_compiled.ma")
					err = copyFile(assemblerBackup, tmpFile)
					if err == nil {
						output = fmt.Sprintf("%s, Assembler file available as %s", output, assemblerBackup)
					} else {
						output = fmt.Sprintf("%s, Assembler file could not be copied for inspection, %s", output, err.Error())
					}
				}

				printTestResult(stateOut, output)

				os.Remove(tmpFile)

			} else if strings.HasSuffix(f.Name(), ".ma") {
				output = fmt.Sprintf("%s%s (Assembler", output, f.Name())

				state, testOut, inses := performAutotest(path.Join(dir, f.Name()), counter, libraries)
				stateOut = state
				output = fmt.Sprintf("%s, %s", output, testOut)

				if inses > 0 {
					perfTrace += inses
				}

				if state == aurora.Red("FAIL").String() { // WTF 2
					failedTotal++
				}

				printTestResult(stateOut, output)

			} else {
				output = fmt.Sprintf("%s%s (Unknown file extension)", output, f.Name())
				stateOut = aurora.Gray("SKIP").String()
				printTestResult(stateOut, output)
			}
		}
	}

	log.Println()
	log.Println(aurora.Cyan(aurora.Bold("Autotest Summary:")))
	log.Println(aurora.Gray(fmt.Sprintf("Tests total:  %d", counter+1)))
	log.Println(aurora.Green(fmt.Sprintf("Tests passed: %d", (counter+1)-failedTotal)))
	log.Println(aurora.Red(fmt.Sprintf("Tests failed: %d", failedTotal)))
	log.Printf("Performance trace: %s\n", aurora.Bold(strconv.Itoa(perfTrace)))

	if failedTotal == 0 {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}

func printTestResult(state, output string) {
	log.Printf("[%s] %s)\r\n", aurora.Bold(state), output)
}

func callMscr(input, output string, optimizeDisable bool) (success bool, state, mscrLog string) {

	mscrLogWriterString := ""
	mscrLogWriter := bytes.NewBufferString(mscrLogWriterString)
	log.SetOutput(mscrLogWriter)

	successChan := make(chan bool)

	go func() {
		defer func() {
			if p := recover(); p != nil {
				log.Println()
				log.Println(p)
				successChan <- false
			}
		}()
		mscr.CompileMSCR(input, output, true, false, optimizeDisable)
		successChan <- true
	}()

	success = <-successChan
	log.SetOutput(os.Stdout)

	if success {
		state = aurora.Blue("MSCR").String()
	} else {
		state = aurora.Red("FAIL").String()
	}

	mscrLog = mscrLogWriter.String()

	return
}

func performAutotest(file string, counter int, libraries []string) (state, result string, instructions int) {

	result = ""
	instructions = -1

	fileContents, err := ioutil.ReadFile(file)

	if err != nil {
		log.Fatalln("Couldn't read file that existed when tests started. Check permissions and try again.")
	}

	// Extract autotest header
	validHeader, register, expected := extractAutotestHeader(string(fileContents))

	if !validHeader {
		state = aurora.Gray("SK_H").String()
		result = "Invalid autotest header"
		return
	}

	// Call assembler
	tmpFile := path.Join(os.TempDir(), "mcpc_autotest.mb")
	assemblerSuccess, mcpcLog := callMcpcAssembler(file, tmpFile, libraries)

	if !assemblerSuccess {
		log.Printf(aurora.Bold("Test %d: vvvvv MCPC failed to assemble, output log below this line vvvvv\r\n").String(), counter)
		fmt.Println(mcpcLog)

		result = "Assembler failure"
		state = aurora.Red("FAIL").String()

		return
	}

	// Read assembly
	assembly, err := ioutil.ReadFile(tmpFile)
	if err != nil {
		log.Fatalln("Couldn't read output file of assembler. Check permissions in temp-directory and try again.")
	}

	// Parse data into instruction-bounded array
	data16 := make([]uint16, len(assembly)/2)
	for i := 0; i < len(data16); i++ {
		data16[i] = uint16(assembly[i*2])<<8 | uint16(assembly[i*2+1])
	}

	vm := interpreter.NewVM(data16, 98, 35)

	steps := 0
	for !vm.Halted {
		_, err := vm.Step()

		if err != nil {
			state = aurora.Red("FAIL").String()
			result = "Error during VM step, " + err.Error()
			return
		}

		steps++

		if steps > 100000 {
			state = aurora.Red("FAIL").String()
			result = "Timeout during VM execution"
			return
		}
	}

	// Validate result
	reg := interpreter.GetReg(vm, register<<4, 0x00F0)
	if reg.Value != expected {
		state = aurora.Red("FAIL").String()
		result = fmt.Sprintf("Value mismatch, actual: %d, expected: %d", reg.Value, expected)
		return
	}

	state = aurora.Green("PASS").String()
	result = fmt.Sprintf("Expected value (%d) matched, steps: %d", expected, steps)
	instructions = steps

	return
}

// Call self in assemble mode to generate binary output from input file (for use with VM)
func callMcpcAssembler(input, output string, libraries []string) (success bool, mcpcLog string) {
	for i := 0; i < len(libraries); i++ {
		libraries[i] = "--library=" + libraries[i]
	}

	parameter := append([]string{"assemble", "--debug-symbols", input, output}, libraries...)
	cmd := exec.Command(os.Args[0], parameter...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		mcpcLog = string(out)
		success = true
	} else {
		if out == nil || len(out) == 0 {
			mcpcLog = err.Error()
		} else {
			mcpcLog = string(out)
		}

		success = false
	}

	return
}

// Extract data from "(;|//)autotest (reg|val)=(0x)?\d" header
func extractAutotestHeader(fileContents string) (valid bool, register, expected uint16) {

	registerMatch := regexpRegister.FindAllStringSubmatch(fileContents, 1)
	if registerMatch == nil {
		return false, 0, 0
	}

	expectedMatch := regexpExpected.FindAllStringSubmatch(fileContents, 1)
	if expectedMatch == nil {
		return false, 0, 0
	}

	if strings.HasPrefix(expectedMatch[0][1], "0x") {
		regTmp, err := strconv.ParseUint(expectedMatch[0][1][2:], 16, 16)
		if err != nil {
			return false, 0, 0
		} else {
			expected = uint16(regTmp)
		}
	} else {
		regTmp, err := strconv.ParseUint(expectedMatch[0][1], 10, 16)
		if err != nil {
			return false, 0, 0
		} else {
			expected = uint16(regTmp)
		}
	}

	if strings.HasPrefix(registerMatch[0][1], "0x") {
		regTmp, err := strconv.ParseUint(registerMatch[0][1][2:], 16, 16)
		if err != nil {
			return false, 0, 0
		} else {
			register = uint16(regTmp)
		}
	} else {
		regTmp, err := strconv.ParseUint(registerMatch[0][1], 10, 16)
		if err != nil {
			return false, 0, 0
		} else {
			register = uint16(regTmp)
		}
	}

	return true, register, expected
}

func copyFile(dst, src string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	// no need to check errors on read only file, we already got everything
	// we need from the filesystem, so nothing can go wrong now.
	defer s.Close()
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(d, s); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}
