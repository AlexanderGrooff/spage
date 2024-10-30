package pkg

import "fmt"

func DebugOutput(format string, s ...any) {
	fmt.Printf("\033[34m"+format+"\033[0m\n", s...)
}

func PPrintOutput(output ModuleOutput, err error) {
	if err != nil {
		if output == nil {
			fmt.Printf("  \033[31m%s\033[0m\n", err)
		} else {
			fmt.Printf("\033[31m%s\033[0m\n", output.String())
		}
	} else if output.Changed() {
		// Yellow
		fmt.Printf("\033[33m%s\033[0m\n", output.String())
	} else {
		// Green
		fmt.Printf("\033[32m%s\033[0m\n", output.String())
	}
}
