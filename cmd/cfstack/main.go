/*
Copyright © 2019 NAME HERE <EMAIL ADDRESS>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"fmt"
	"github.com/CleverTap/cfstack/internal/pkg/cmd/cfstack"
	"os"
	"strings"
)

// Set by build process
var (
	version string
)

func main() {
	// Look for version
	for _, v := range os.Args[1:] {
		v = strings.TrimLeft(v, "-")
		if v == "v" || v == "version" {
			if version == "" {
				version = "dev"
			}

			fmt.Printf("version %s\n", version)
			os.Exit(0)
		}
	}
	cfstack.Execute()
}
