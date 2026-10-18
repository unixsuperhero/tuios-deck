package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/unixsuperhero/tuios-deck/internal/config"
)

const configUsage = `deck config: the settings file

Usage:
  deck config <command>

Commands:
  path                 Print the settings file
  show                 Print the settings in effect (the defaults for a missing file)
  get <setting>        Print one setting
  set <setting> <val>  Change one setting and save the file
  init                 Write the file with the defaults when it does not exist

Settings:
  vim_modes            true or false: forms start in insert mode, esc goes to normal mode, esc again cancels
`

func runConfig(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(configUsage)
		if len(args) == 0 {
			os.Exit(2)
		}
		return nil
	}
	path := config.DefaultPath()
	c, err := config.Load(path)
	if err != nil {
		return err
	}
	switch args[0] {
	case "path":
		fmt.Println(path)
	case "show":
		data, _ := json.MarshalIndent(c, "", "  ")
		fmt.Println(string(data))
	case "get":
		if len(args) != 2 {
			return fmt.Errorf("get takes one setting")
		}
		switch args[1] {
		case "vim_modes":
			fmt.Println(c.VimModes)
		default:
			return fmt.Errorf("unknown setting %q", args[1])
		}
	case "set":
		if len(args) != 3 {
			return fmt.Errorf("set takes a setting and a value")
		}
		switch args[1] {
		case "vim_modes":
			b, err := strconv.ParseBool(args[2])
			if err != nil {
				return fmt.Errorf("vim_modes is true or false, not %q", args[2])
			}
			c.VimModes = b
		default:
			return fmt.Errorf("unknown setting %q", args[1])
		}
		if err := c.Save(path); err != nil {
			return err
		}
		fmt.Println("saved", path)
	case "init":
		if _, err := os.Stat(path); err == nil {
			fmt.Println("exists", path)
			return nil
		}
		if err := c.Save(path); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	default:
		fmt.Print(configUsage)
		return fmt.Errorf("unknown config command %q", args[0])
	}
	return nil
}
