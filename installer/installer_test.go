package installer

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

/*
 * The installer script, checked for the one class of mistake its assembler
 * cannot catch.
 *
 * NSIS conditionals take *jump targets*, and a target may be a label or a
 * signed instruction count. A label that does not exist is a compile error. A
 * count is always arithmetically valid, so every count compiles — including
 * one that lands past the end of the function it is written in.
 *
 * v0.9.29 shipped `IfErrors 0 +3` inside LaunchAsUser, where +1 was a
 * MessageBox, +2 was the Return that FunctionEnd compiles to, and +3 was the
 * first instruction of the next function. On the success path it skipped its
 * own return and fell into StartTray, which calls LaunchAsUser again:
 * unbounded recursion that started a LANcast on every pass. Thirty in under
 * thirty seconds, and the machine went down with them.
 *
 * `makensis -WX` passed. It had to -- there is nothing there to warn about.
 * So the rule is enforced here instead: in this file, conditionals name a
 * label. It costs nothing and it is the only check that could have caught it.
 */
func TestConditionalsJumpToLabelsNotOffsets(t *testing.T) {
	script, err := os.ReadFile("lancast.nsi")
	if err != nil {
		t.Fatalf("read the installer script: %v", err)
	}

	// The NSIS instructions that take jump targets. Each one can carry a
	// relative count, and each one is a chance to count wrong.
	conditionals := regexp.MustCompile(
		`(?i)^\s*(IfErrors|IfFileExists|IfSilent|IfRebootFlag|IfAbort|StrCmp|StrCmpS|IntCmp|IntCmpU|Int64Cmp|Int64CmpU|IfShellVarContextAll|SectionFlagIsSet)\b`)
	// A target of +N or -N. Zero is not a jump at all -- it means "carry on" --
	// so it is the one numeric target that is safe and idiomatic.
	offset := regexp.MustCompile(`\s[+-][1-9]\d*\b`)

	for i, line := range strings.Split(string(script), "\n") {
		code, _, _ := strings.Cut(line, ";") // drop trailing comments
		if !conditionals.MatchString(code) {
			continue
		}
		if m := offset.FindString(code); m != "" {
			t.Errorf("lancast.nsi:%d jumps by %s instead of to a label:\n\t%s\n\n"+
				"A relative jump compiles whatever number it is given, so nothing "+
				"upstream can tell a correct one from one that lands in the next "+
				"function. Name a label instead.",
				i+1, strings.TrimSpace(m), strings.TrimSpace(line))
		}
	}
}

/*
 * The specific function that did it, checked by name.
 *
 * The rule above would catch a recurrence anywhere. This one exists because
 * LaunchAsUser is called from all three finish-page paths, so a mistake in it
 * is a mistake in every way there is to finish the installer -- and because a
 * general rule is easy to weaken later, while a test naming the thing that
 * actually happened is harder to argue with.
 */
func TestLaunchAsUserReturnsOnSuccess(t *testing.T) {
	script, err := os.ReadFile("lancast.nsi")
	if err != nil {
		t.Fatalf("read the installer script: %v", err)
	}
	body := functionBody(string(script), "LaunchAsUser")
	if body == "" {
		t.Fatal("LaunchAsUser is gone; the finish page starts LANcast some other way now")
	}
	if !strings.Contains(body, "IfErrors 0 launched") {
		t.Errorf("LaunchAsUser no longer branches to the `launched` label:\n%s", body)
	}
	if !strings.Contains(body, "launched:") {
		t.Errorf("the `launched` label is missing, so the success path has nowhere to land:\n%s", body)
	}
}

// functionBody returns the lines between `Function <name>` and its FunctionEnd.
func functionBody(script, name string) string {
	lines := strings.Split(script, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "Function "+name) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "FunctionEnd" {
			return strings.Join(lines[start:i+1], "\n")
		}
	}
	return ""
}
