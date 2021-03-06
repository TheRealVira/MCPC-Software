package compiler

const AssigneableRegisters = 4

// Parameter types for meta-assembly
// An asmCmd with only asmParamTypeRaw-type parameters is considered "fully resolved"
const asmParamTypeRaw = 0
const asmParamTypeVarRead = 1
const asmParamTypeVarWrite = 2
const asmParamTypeCalc = 4
const asmParamTypeGlobalWrite = 8
const asmParamTypeGlobalRead = 16
const asmParamTypeScopeVarCount = 32
const asmParamTypeStringRead = 64
const asmParamTypeVarAddr = 128
const asmParamTypeStringAddr = 256
const asmParamTypeGlobalAddr = 512

type asmCmd struct {
	ins    string
	params []*asmParam

	// Encompassing function name
	scope string

	// For meta-assembly-only commands; these will never be directly represented in output asm
	scopeAnnotationName     string
	scopeAnnotationRegister int

	// For output formatting
	comment     string
	printIndent int

	// For verbose printing
	originalAsmCmdString string
}

type asmParam struct {
	asmParamType int
	value        string

	// For resolving globals and strings
	addrCache int
}

type asmTransformState struct {
	currentFunction           string
	currentScopeVariableCount int

	functionTableVar  []string
	functionTableVoid []string

	globalMemoryMap map[string]int
	maxDataAddr     int

	variableMap map[string][]asmVar
	stringMap   map[string]int

	specificInitializationAsm []*asmCmd
	binData                   []int16

	scopeRegisterAssignment  map[string]int
	scopeRegisterDirty       map[int]bool
	scopeVariableDirectMarks map[string]bool

	printIndent int
	verbose     bool
}

type asmVar struct {
	name        string
	orderNumber int
	isGlobal    bool
}
