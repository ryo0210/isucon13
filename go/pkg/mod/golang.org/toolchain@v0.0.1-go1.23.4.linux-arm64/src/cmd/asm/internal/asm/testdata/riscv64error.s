// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

TEXT errors(SB),$0
	MOV	$errors(SB), (X5)		// ERROR "address load must target register"
	MOV	$8(SP), (X5)			// ERROR "address load must target register"
	MOVB	$8(SP), X5			// ERROR "unsupported address load"
	MOVH	$8(SP), X5			// ERROR "unsupported address load"
	MOVW	$8(SP), X5			// ERROR "unsupported address load"
	MOVF	$8(SP), X5			// ERROR "unsupported address load"
	MOV	$1234, 0(SP)			// ERROR "constant load must target register"
	MOV	$1234, 8(SP)			// ERROR "constant load must target register"
	MOV	$0, 0(SP)			// ERROR "constant load must target register"
	MOV	$0, 8(SP)			// ERROR "constant load must target register"
	MOV	$1234, 0(SP)			// ERROR "constant load must target register"
	MOV	$1234, 8(SP)			// ERROR "constant load must target register"
	MOVB	$1, X5				// ERROR "unsupported constant load"
	MOVH	$1, X5				// ERROR "unsupported constant load"
	MOVW	$1, X5				// ERROR "unsupported constant load"
	MOVF	$1, X5				// ERROR "unsupported constant load"
	MOVBU	X5, (X6)			// ERROR "unsupported unsigned store"
	MOVHU	X5, (X6)			// ERROR "unsupported unsigned store"
	MOVWU	X5, (X6)			// ERROR "unsupported unsigned store"
	MOVF	F0, F1, F2			// ERROR "illegal MOV instruction"
	MOVD	F0, F1, F2			// ERROR "illegal MOV instruction"
	MOV	X10, X11, X12			// ERROR "illegal MOV instruction"
	MOVW	X10, X11, X12			// ERROR "illegal MOV instruction"
	RORI	$64, X5, X6			// ERROR "immediate out of range 0 to 63"
	SLLI	$64, X5, X6			// ERROR "immediate out of range 0 to 63"
	SRLI	$64, X5, X6			// ERROR "immediate out of range 0 to 63"
	SRAI	$64, X5, X6			// ERROR "immediate out of range 0 to 63"
	RORI	$-1, X5, X6			// ERROR "immediate out of range 0 to 63"
	SLLI	$-1, X5, X6			// ERROR "immediate out of range 0 to 63"
	SRLI	$-1, X5, X6			// ERROR "immediate out of range 0 to 63"
	SRAI	$-1, X5, X6			// ERROR "immediate out of range 0 to 63"
	RORIW	$32, X5, X6			// ERROR "immediate out of range 0 to 31"
	SLLIW	$32, X5, X6			// ERROR "immediate out of range 0 to 31"
	SRLIW	$32, X5, X6			// ERROR "immediate out of range 0 to 31"
	SRAIW	$32, X5, X6			// ERROR "immediate out of range 0 to 31"
	RORIW	$-1, X5, X6			// ERROR "immediate out of range 0 to 31"
	SLLIW	$-1, X5, X6			// ERROR "immediate out of range 0 to 31"
	SRLIW	$-1, X5, X6			// ERROR "immediate out of range 0 to 31"
	SRAIW	$-1, X5, X6			// ERROR "immediate out of range 0 to 31"
	SD	X5, 4294967296(X6)		// ERROR "constant 4294967296 too large"
	SRLI	$1, X5, F1			// ERROR "expected integer register in rd position but got non-integer register F1"
	SRLI	$1, F1, X5			// ERROR "expected integer register in rs1 position but got non-integer register F1"
	FNES	F1, (X5)			// ERROR "needs an integer register output"
	RET