package main

import (
	"fmt"
	"github.com/mitchellh/go-mruby"
)

func main() {
	mrb := mruby.NewMrb()
	defer mrb.Close()

	result, err := mrb.LoadString(`["Hello", "from", "!yburm".reverse].join(' ')`)

	if err != nil {
		panic(err.Error())
	}

	fmt.Printf("Result: %s\n", result.String())
}
