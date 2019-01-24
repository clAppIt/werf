package templates

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"
	"unicode"

	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"

	"github.com/flant/werf/cmd/werf/common"
	"github.com/flant/werf/pkg/logger/terminal"
)

type FlagExposer interface {
	ExposeFlags(cmd *cobra.Command, flags ...string) FlagExposer
}

func ActsAsRootCommand(cmd *cobra.Command, groups ...CommandGroup) FlagExposer {
	t := &templater{
		RootCmd:       cmd,
		UsageTemplate: MainUsageTemplate(),
		HelpTemplate:  MainHelpTemplate(),
		CommandGroups: groups,
	}
	cmd.SetUsageFunc(t.UsageFunc())
	cmd.SetHelpFunc(t.HelpFunc())
	return t
}

type templater struct {
	UsageTemplate string
	HelpTemplate  string
	RootCmd       *cobra.Command
	CommandGroups
}

func (t *templater) ExposeFlags(cmd *cobra.Command, flags ...string) FlagExposer {
	cmd.SetUsageFunc(t.UsageFunc(flags...))
	return t
}

func (t *templater) HelpFunc() func(*cobra.Command, []string) {
	return func(c *cobra.Command, s []string) {
		tpl := template.New("help")
		tpl.Funcs(t.templateFuncs())
		template.Must(tpl.Parse(t.HelpTemplate))
		err := tpl.Execute(os.Stdout, c)
		if err != nil {
			c.Println(err)
		}
	}
}

func (t *templater) UsageFunc(exposedFlags ...string) func(*cobra.Command) error {
	return func(c *cobra.Command) error {
		tpl := template.New("usage")
		tpl.Funcs(t.templateFuncs(exposedFlags...))
		template.Must(tpl.Parse(t.UsageTemplate))
		return tpl.Execute(os.Stdout, c)
	}
}

func (t *templater) templateFuncs(exposedFlags ...string) template.FuncMap {
	return template.FuncMap{
		"trim":                strings.TrimSpace,
		"trimRight":           func(s string) string { return strings.TrimRightFunc(s, unicode.IsSpace) },
		"trimLeft":            func(s string) string { return strings.TrimLeftFunc(s, unicode.IsSpace) },
		"gt":                  cobra.Gt,
		"eq":                  cobra.Eq,
		"rpad":                rpad,
		"appendIfNotPresent":  appendIfNotPresent,
		"flagsNotIntersected": flagsNotIntersected,
		"visibleFlags":        visibleFlags,
		"flagsUsages":         FlagsUsages,
		"cmdGroups":           t.cmdGroups,
		"cmdGroupsString":     t.cmdGroupsString,
		"rootCmd":             t.rootCmdName,
		"isRootCmd":           t.isRootCmd,
		"optionsCmdFor":       t.optionsCmdFor,
		"usageLine":           t.usageLine,
		"environment": func(c *cobra.Command) string {
			if res, ok := c.Annotations[common.CmdEnvAnno]; ok {
				return res
			}

			return ""
		},
		"exposed": func(c *cobra.Command) *flag.FlagSet {
			exposed := flag.NewFlagSet("exposed", flag.ContinueOnError)
			if len(exposedFlags) > 0 {
				for _, name := range exposedFlags {
					if f := c.Flags().Lookup(name); f != nil {
						exposed.AddFlag(f)
					}
				}
			}
			return exposed
		},
	}
}

func (t *templater) cmdGroups(c *cobra.Command, all []*cobra.Command) []CommandGroup {
	if len(t.CommandGroups) > 0 && c == t.RootCmd {
		return AddAdditionalCommands(t.CommandGroups, "Other Commands:", all)
	}
	return []CommandGroup{
		{
			Message:  "Available Commands:",
			Commands: all,
		},
	}
}

func (t *templater) cmdGroupsString(c *cobra.Command) string {
	var groups []string
	for _, cmdGroup := range t.cmdGroups(c, c.Commands()) {
		cmds := []string{cmdGroup.Message}
		for _, cmd := range cmdGroup.Commands {
			if cmd.IsAvailableCommand() {
				cmds = append(cmds, "  "+rpad(cmd.Name(), cmd.NamePadding())+" "+cmd.Short)
			}
		}
		groups = append(groups, strings.Join(cmds, "\n"))
	}
	return strings.Join(groups, "\n\n")
}

func (t *templater) rootCmdName(c *cobra.Command) string {
	return t.rootCmd(c).CommandPath()
}

func (t *templater) isRootCmd(c *cobra.Command) bool {
	return t.rootCmd(c) == c
}

func (t *templater) parents(c *cobra.Command) []*cobra.Command {
	parents := []*cobra.Command{c}
	for current := c; !t.isRootCmd(current) && current.HasParent(); {
		current = current.Parent()
		parents = append(parents, current)
	}
	return parents
}

func (t *templater) rootCmd(c *cobra.Command) *cobra.Command {
	if c != nil && !c.HasParent() {
		return c
	}
	if t.RootCmd == nil {
		panic("nil root cmd")
	}
	return t.RootCmd
}

func (t *templater) optionsCmdFor(c *cobra.Command) string {
	if !c.Runnable() {
		return ""
	}
	rootCmdStructure := t.parents(c)
	for i := len(rootCmdStructure) - 1; i >= 0; i-- {
		cmd := rootCmdStructure[i]
		if _, _, err := cmd.Find([]string{"options"}); err == nil {
			return cmd.CommandPath() + " options"
		}
	}
	return ""
}

func (t *templater) usageLine(c *cobra.Command) string {
	return UsageLine(c)
}

func UsageLine(c *cobra.Command) string {
	usage := c.UseLine()
	suffix := "[options]"
	if c.HasFlags() && !strings.Contains(usage, suffix) {
		usage += " " + suffix
	}
	return usage
}

func FlagsUsages(f *flag.FlagSet) string {
	x := new(bytes.Buffer)

	f.VisitAll(func(flag *flag.Flag) {
		if flag.Hidden {
			return
		}

		leftPart := flagLeftPart(flag)
		rightPart := terminal.FitTextWithIndent(flag.Usage, 12)

		fmt.Fprintf(x, "%s:\n%s\n", leftPart, rightPart)
	})

	return x.String()
}

func flagLeftPart(flag *flag.Flag) string {
	format := "--%s=%s"

	if flag.Value.Type() == "string" {
		format = "--%s='%s'"
	}

	if len(flag.Shorthand) > 0 {
		format = "  -%s, " + format
		return fmt.Sprintf(format, flag.Shorthand, flag.Name, flag.DefValue)
	} else {
		format = "      " + format
		return fmt.Sprintf(format, flag.Name, flag.DefValue)
	}
}

func rpad(s string, padding int) string {
	t := fmt.Sprintf("%%-%ds", padding)
	return fmt.Sprintf(t, s)
}

func appendIfNotPresent(s, stringToAppend string) string {
	if strings.Contains(s, stringToAppend) {
		return s
	}
	return s + " " + stringToAppend
}

func flagsNotIntersected(l *flag.FlagSet, r *flag.FlagSet) *flag.FlagSet {
	f := flag.NewFlagSet("notIntersected", flag.ContinueOnError)
	l.VisitAll(func(flag *flag.Flag) {
		if r.Lookup(flag.Name) == nil {
			f.AddFlag(flag)
		}
	})
	return f
}

func visibleFlags(l *flag.FlagSet) *flag.FlagSet {
	hidden := "help"
	f := flag.NewFlagSet("visible", flag.ContinueOnError)
	l.VisitAll(func(flag *flag.Flag) {
		if flag.Name != hidden {
			f.AddFlag(flag)
		}
	})
	return f
}
