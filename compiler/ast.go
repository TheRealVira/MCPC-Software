package compiler

import (
	"fmt"
	"log"
	"reflect"
	"unicode"
)

func (ast *AST) GenerateASM() string {

	log.Println("Generating ASM...")

	/*fmt.Println("AST:")
	walkInterface(ast, func(val reflect.Value, name string, depth int) {
		for i := 0; i < depth+1; i++ {
			fmt.Print("  ")
		}
		fmt.Print(name)

		if val.Kind() == reflect.Struct {
			fmt.Println()
		} else if val.Kind() == reflect.Int {
			fmt.Print(" = ")
			fmt.Println(val.Int())
		} else if val.Kind() == reflect.Bool {
			fmt.Print(" = ")
			fmt.Println(val.Bool())
		} else {
			fmt.Print(" = ")
			fmt.Println(val.String())
		}
	}, nil, 0)*/

	asm := ""

	// Redefinition detection tables
	var globalTable []*Global
	var functionTable []string

	// Fill tables
	walkInterface(ast, func(val reflect.Value, name string, depth int) {

		if val.Kind() != reflect.Struct {
			// Early out if value instead of node
			return
		}

		nodeInterface := val.Interface()

		switch node := nodeInterface.(type) {

		case Global:
			for _, g := range globalTable {
				if g.Name == node.Name {
					log.Fatalf("Redefinition of global '%s' at %s\n", node.Name, node.Pos.String())
				}
			}
			globalTable = append(globalTable, &node)

		case Function:
			functionLabel := fmt.Sprintf("mscr_function_%s_%s_params_%d", node.Type, node.Name, len(node.Parameters))
			for _, f := range functionTable {
				if f == functionLabel {
					log.Fatalf("Redefinition of function '%s' at %s\n", node.Name, node.Pos.String())
				}
			}
			functionTable = append(functionTable, functionLabel)
		}

	}, nil, 0)

	// Check for entry point existance
	containsMain := false
	for _, f := range functionTable {
		if f == "mscr_function_var_main_params_2" {
			containsMain = true
			break
		}
	}
	if !containsMain {
		log.Fatalln("Entry point not found: Please declare a function 'func var main (argc, argp)'")
	}

	transformState := &asmTransformState{
		functionTable:   functionTable,
		currentFunction: "",

		globalMemoryMap: make(map[string]int, 0),
		maxGlobalAddr:   0,
	}

	// Output ASM
	walkInterface(ast, func(val reflect.Value, name string, depth int) {

		if val.Kind() != reflect.Struct {
			// Early out if value instead of node
			return
		}

		nodeInterface := val.Interface()
		newAsm := asmForNodePre(nodeInterface, transformState)

		if newAsm != "" {
			asm = fmt.Sprintf("%s\n; %s (func: %s)\n%s", asm, name, transformState.currentFunction, newAsm)
		}

	}, func(val reflect.Value, name string, depth int) {

		if val.Kind() != reflect.Struct {
			// Early out if value instead of node
			return
		}

		nodeInterface := val.Interface()
		asmForNodePost(nodeInterface, transformState)

	}, 0)

	return initializationAsm + asm
}

func walkInterface(x interface{}, pre func(reflect.Value, string, int), post func(reflect.Value, string, int), level int) {
	typ := reflect.TypeOf(x)

	for typ.Kind() == reflect.Ptr {
		x = reflect.ValueOf(x).Elem().Interface()
		typ = reflect.TypeOf(x)
	}

	if typ.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < typ.NumField(); i++ {
		switch typ.Field(i).Type.Kind() {
		case reflect.Slice:
			s := reflect.ValueOf(x).Field(i)
			styp := reflect.TypeOf(x).Field(i)
			if s.Type().Kind() == reflect.Ptr && s.IsNil() {
				continue
			}

			for j := 0; j < s.Len(); j++ {
				s2 := s.Index(j)

				for s2.Kind() == reflect.Ptr {
					s2 = s2.Elem()
				}

				if pre != nil {
					pre(s2, styp.Name, level)
				}
				walkInterface(s2.Interface(), pre, post, level+1)
				if post != nil {
					post(s2, styp.Name, level)
				}
			}

		default:
			s := reflect.ValueOf(x).Field(i)
			styp := reflect.TypeOf(x).Field(i)
			if s.Type().Kind() == reflect.Ptr && s.IsNil() {
				continue
			}

			for s.Kind() == reflect.Ptr {
				s = s.Elem()
			}

			if pre != nil {
				pre(s, styp.Name, level)
			}

			// Check exported status
			fletter := []rune(styp.Name)[0]
			if unicode.IsLetter(fletter) && unicode.IsUpper(fletter) {
				walkInterface(s.Interface(), pre, post, level+1)
			}

			if post != nil {
				post(s, styp.Name, level)
			}
		}
	}
}

const initializationAsm = `
; Generated by the MSCR compiler

; MSCR initialization routine
.mscr_init_main __LABEL_SET
SET SP
0x7FFE ; highest memory location - 1

CALL mscr_function_var_main_params_2 ; Call userland main

HALT ; After execution, halt

`
