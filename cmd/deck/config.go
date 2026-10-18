package main

import (
	"encoding/json"
	"fmt"
	"os"

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
  keys                 List every mode's actions with their default and current keys
  bind <mode> <action> <key>   Rebind an action; the default key (or "") restores it
  unbind <mode> <action>       Restore an action's default key
  init                 Write the file with the defaults when it does not exist

Settings:
  stale_after          a duration (default 45m; 0 turns it off): a pane with no activity for this long is marked stale
Modes for bind: deck, form_normal, form_insert, live (see "deck config keys")
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
		case "stale_after":
			fmt.Println(c.StaleThreshold())
		default:
			return fmt.Errorf("unknown setting %q", args[1])
		}
	case "set":
		if len(args) != 3 {
			return fmt.Errorf("set takes a setting and a value")
		}
		switch args[1] {
		case "stale_after":
			c.StaleAfter = args[2]
		default:
			return fmt.Errorf("unknown setting %q", args[1])
		}
		if err := c.Save(path); err != nil {
			return err
		}
		fmt.Println("saved", path)
	case "keys":
		for _, mode := range config.Modes() {
			fmt.Println(mode)
			for _, a := range config.ActionsOf(mode) {
				fmt.Printf("  %-13s default %-10s now %-10s %s\n", a.Name, a.Default, c.Binding(mode, a.Name), a.Desc)
			}
		}
	case "bind", "unbind":
		key := ""
		if args[0] == "bind" {
			if len(args) != 4 {
				return fmt.Errorf("bind takes a mode, an action and a key")
			}
			key = args[3]
		} else if len(args) != 3 {
			return fmt.Errorf("unbind takes a mode and an action")
		}
		if err := c.Bind(args[1], args[2], key); err != nil {
			return err
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
