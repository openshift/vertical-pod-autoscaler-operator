package main

import (
	"bytes"
	"os"
	"sigs.k8s.io/yaml"
)

func main() {
	var err error
	r := os.Stdin
	if len(os.Args) > 1 {
		r, err = os.Open(os.Args[1])
		if err != nil {
			panic(err)
		}
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(r)
	json, err := yaml.JSONToYAML(buf.Bytes())
	if err != nil {
		panic(err)
	}
	os.Stdout.Write(json[:])
}
