package yson

import (
	"fmt"
	"reflect"

	"github.com/go-faster/errors"
)

var ErrInvalidNesting = errors.New("invalid YSON nesting")

type UnsupportedTypeError struct {
	UserType reflect.Type
}

func (e *UnsupportedTypeError) Error() string {
	return "yson: value of type " + e.UserType.String() + " is not supported"
}

type TypeError struct {
	UserType reflect.Type
	YSONType Type

	Struct string
	Field  string
}

func (e *TypeError) Error() string {
	msg := "yson: cannot unmarshal " + e.YSONType.String() + " into value of type " + e.UserType.String()
	if e.Struct != "" {
		msg += " at " + e.Struct + "." + e.Field
	}
	return msg
}

type SyntaxError struct {
	Message string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("yson: syntax error: %s", e.Message)
}
