
; Generated by the MSCR compiler

; MSCR initialization routine
.mscr_init_main __LABEL_SET
SET SP
0x7FFE ; highest memory location - 1

CALL mscr_function_var_main_params_2 ; Call userland main

HALT ; After execution, halt


; Function (func: putchar)
.mscr_function_void_putchar_params_1 __LABEL_SET

; Body (func: putchar)

        MEMW A B
        MOV A A
        MOV B B
    
; Function (func: alphabet)
.mscr_function_void_alphabet_params_0 __LABEL_SET

; Function (func: main)
.mscr_function_var_main_params_2 __LABEL_SET
