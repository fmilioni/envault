package cli

import (
	"fmt"
	"io"
	"strings"
)

type diffOp int

const (
	diffContext diffOp = iota
	diffDel
	diffAdd
)

type diffLine struct {
	op   diffOp
	text string
}

// lineDiff produces a line-level unified diff of a→b via a standard LCS walk.
func lineDiff(aText, bText string) []diffLine {
	a, b := splitLines(aText), splitLines(bText)
	n, m := len(a), len(b)

	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var out []diffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, diffLine{diffContext, a[i]})
			i, j = i+1, j+1
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, diffLine{diffDel, a[i]})
			i++
		default:
			out = append(out, diffLine{diffAdd, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, diffLine{diffDel, a[i]})
	}
	for ; j < m; j++ {
		out = append(out, diffLine{diffAdd, b[j]})
	}
	return out
}

func renderDiff(w io.Writer, lines []diffLine) {
	for _, l := range lines {
		switch l.op {
		case diffDel:
			fmt.Fprintf(w, "    - %s\n", l.text)
		case diffAdd:
			fmt.Fprintf(w, "    + %s\n", l.text)
		default:
			fmt.Fprintf(w, "      %s\n", l.text)
		}
	}
}

// splitLines drops the empty element a trailing newline produces, so a file
// ending in "\n" doesn't diff as having a phantom blank last line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
