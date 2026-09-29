package bytecode

import (
	"errors"
	"math"
	"testing"
)

func TestArithmeticOverflow(t *testing.T) {
	if _, err := AddInt(math.MaxInt64, 1); !errors.Is(err, ErrOverflow{}) {
		t.Fatalf("max+1 must overflow, got %v", err)
	}
	if _, err := AddInt(math.MinInt64, -1); !errors.Is(err, ErrOverflow{}) {
		t.Fatalf("min+-1 must overflow")
	}
	if r, err := AddInt(math.MaxInt64, 0); err != nil || r != math.MaxInt64 {
		t.Fatalf("max+0 = %d,%v", r, err)
	}

	if _, err := SubInt(math.MinInt64, 1); !errors.Is(err, ErrOverflow{}) {
		t.Fatalf("min-1 must overflow")
	}

	if _, err := MulInt(math.MaxInt64, 2); !errors.Is(err, ErrOverflow{}) {
		t.Fatalf("max*2 must overflow")
	}
	if _, err := MulInt(math.MinInt64, -1); !errors.Is(err, ErrOverflow{}) {
		t.Fatalf("min*-1 must overflow")
	}

	if _, err := DivInt(1, 0); !errors.Is(err, ErrDivByZero{}) {
		t.Fatalf("div by zero")
	}
	if _, err := DivInt(math.MinInt64, -1); !errors.Is(err, ErrOverflow{}) {
		t.Fatalf("min/-1 must overflow")
	}

	if _, err := ModInt(1, 0); !errors.Is(err, ErrDivByZero{}) {
		t.Fatalf("mod by zero")
	}

	if _, err := NegInt(math.MinInt64); !errors.Is(err, ErrOverflow{}) {
		t.Fatalf("neg min must overflow")
	}
	if r, err := NegInt(-5); err != nil || r != 5 {
		t.Fatalf("neg -5 = %d,%v", r, err)
	}
}

func TestOpcodeMetadata(t *testing.T) {
	if OpAdd.StackEffect() != -1 || OpPush.StackEffect() != 1 {
		t.Fatalf("stack effects wrong")
	}
	if !OpJz.IsBranch() || OpJz.IsTerminator() {
		t.Fatalf("branch metadata wrong")
	}
	if !OpRet.IsTerminator() || OpRet.IsBranch() {
		t.Fatalf("terminator metadata wrong")
	}
	if OpLabel.Cost() != 0 || OpDiv.Cost() <= OpAdd.Cost() {
		t.Fatalf("cost metadata wrong")
	}
}
