package mruby

import (
	"fmt"
	"unsafe"
)

// #include <stdlib.h>
// #include "gomruby.h"
import "C"

// Value is an interface that should be implemented by anything that can
// be represents as an mruby value.
type Value interface {
	MrbValue(*Mrb) *MrbValue
}

type Int int
type NilType [0]byte
type String string

// Nil is a constant that can be used as a Nil Value
var Nil NilType

// MrbValue is a "value" internally in mruby. A "value" is what mruby calls
// basically anything in Ruby: a class, an object (instance), a variable,
// etc.
type MrbValue struct {
	value C.mrb_value
	state *C.mrb_state
}

// ValueType is an enum of types that a Value can be and is returned by
// Value.Type().
type ValueType uint32

const (
	TypeFalse ValueType = iota
	TypeFree
	TypeTrue
	TypeFixnum
	TypeSymbol
	TypeUndef
	TypeFloat
	TypeCptr
	TypeObject
	TypeClass
	TypeModule
	TypeIClass
	TypeSClass
	TypeProc
	TypeArray
	TypeHash
	TypeString
	TypeRange
	TypeException
	TypeFile
	TypeEnv
	TypeData
	TypeFiber
	TypeMaxDefine
)

func init() {
	Nil = [0]byte{}
}

// Call calls a method with the given name and arguments on this
// value.
func (v *MrbValue) Call(method string, args ...Value) (*MrbValue, error) {
	return v.call(method, args, nil)
}

// CallBlock is the same as call except that it expects the last
// argument to be a Proc that will be passed into the function call.
// It is an error if args is empty or if there is no block on the end.
func (v *MrbValue) CallBlock(method string, args ...Value) (*MrbValue, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("args must be non-empty and have a proc at the end")
	}

	n := len(args)
	return v.call(method, args[:n-1], args[n-1])
}

func (v *MrbValue) call(method string, args []Value, block Value) (*MrbValue, error) {
	var argv []C.mrb_value = nil
	var argvPtr *C.mrb_value = nil

	if len(args) > 0 {
		// Make the raw byte slice to hold our arguments we'll pass to C
		argv = make([]C.mrb_value, len(args))
		for i, arg := range args {
			argv[i] = arg.MrbValue(&Mrb{v.state}).value
		}

		argvPtr = &argv[0]
	}

	var blockV *C.mrb_value
	if block != nil {
		val := block.MrbValue(&Mrb{v.state}).value
		blockV = &val
	}

	cs := C.CString(method)
	defer C.free(unsafe.Pointer(cs))

	// If we have a block, we have to call a separate function to
	// pass a block in. Otherwise, we just call it directly.
	var result C.mrb_value
	if blockV == nil {
		result = C.mrb_funcall_argv(
			v.state,
			v.value,
			C.mrb_intern_cstr(v.state, cs),
			C.mrb_int(len(argv)),
			argvPtr)
	} else {
		result = C.mrb_funcall_with_block(
			v.state,
			v.value,
			C.mrb_intern_cstr(v.state, cs),
			C.mrb_int(len(argv)),
			argvPtr,
			*blockV)
	}
	if v.state.exc != nil {
		return nil, newExceptionValue(v.state)
	}

	return newValue(v.state, result), nil
}

// IsDead tells you if an object has been collected by the GC or not.
func (v *MrbValue) IsDead() bool {
	return C.ushort(C.mrb_object_dead_p(v.state, C._go_mrb_basic_ptr(v.value))) != 0
}

// MrbValue so that *MrbValue implements the "Value" interface.
func (v *MrbValue) MrbValue(*Mrb) *MrbValue {
	return v
}

// Mrb returns the Mrb state for this value.
func (v *MrbValue) Mrb() *Mrb {
	return &Mrb{v.state}
}

// SetProcTargetClass sets the target class where a proc will be executed
// when this value is a proc.
func (v *MrbValue) SetProcTargetClass(c *Class) {
	proc := C._go_mrb_proc_ptr(v.value)
	proc.target_class = c.class
}

func (v *MrbValue) Type() ValueType {
	return ValueType(C._go_mrb_type(v.value))
}

// Exception is a special type of value that represents an error
// and implements the Error interface.
type Exception struct {
	*MrbValue

	// A cache of the string value of the exception. This is set in
	// newExceptionValue so that the exception error string doesn't rely
	// on the mruby state being available.
	cachedString string
}

func (e *Exception) Error() string {
	return e.String()
}

func (e *Exception) String() string {
	if e.cachedString != "" {
		return e.cachedString
	}

	return e.MrbValue.String()
}

//-------------------------------------------------------------------
// Type conversions to Go types
//-------------------------------------------------------------------

// Array returns the Array value of this value. If the Type of the MrbValue
// is not a TypeArray, then this will panic. If the MrbValue has a
// `to_a` function, you must call that manually prior to calling this
// method.
func (v *MrbValue) Array() *Array {
	return &Array{v}
}

// Fixnum returns the numeric value of this object if the Type() is
// TypeFixnum. Calling this with any other type will result in undefined
// behavior.
func (v *MrbValue) Fixnum() int {
	return int(C._go_mrb_fixnum(v.value))
}

// Float returns the numeric value of this object if the Type() is
// TypeFloat. Calling this with any other type will result in undefined
// behavior.
func (v *MrbValue) Float() float64 {
	return float64(C._go_mrb_float(v.value))
}

// Hash returns the Hash value of this value. If the Type of the MrbValue
// is not a ValueTypeHash, then this will panic. If the MrbValue has a
// `to_h` function, you must call that manually prior to calling this
// method.
func (v *MrbValue) Hash() *Hash {
	return &Hash{v}
}

// String returns the "to_s" result of this value.
func (v *MrbValue) String() string {
	value := C.mrb_obj_as_string(v.state, v.value)
	result := C.GoString(C.mrb_string_value_ptr(v.state, value))
	return result
}

//-------------------------------------------------------------------
// Native Go types implementing the Value interface
//-------------------------------------------------------------------

func (i Int) MrbValue(m *Mrb) *MrbValue {
	return m.FixnumValue(int(i))
}

func (NilType) MrbValue(m *Mrb) *MrbValue {
	return m.NilValue()
}

func (s String) MrbValue(m *Mrb) *MrbValue {
	return m.StringValue(string(s))
}

//-------------------------------------------------------------------
// Internal Functions
//-------------------------------------------------------------------

func newExceptionValue(s *C.mrb_state) *Exception {
	if s.exc == nil {
		panic("exception value init without exception")
	}

	// Convert the RObject* to an mrb_value
	value := C.mrb_obj_value(unsafe.Pointer(s.exc))

	result := newValue(s, value)
	return &Exception{MrbValue: result, cachedString: result.String()}
}

func newValue(s *C.mrb_state, v C.mrb_value) *MrbValue {
	return &MrbValue{
		state: s,
		value: v,
	}
}
