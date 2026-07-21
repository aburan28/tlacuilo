// Package tlc runs the TLC model checker (from tla2tools.jar) and parses
// its machine-readable "-tool" output into typed results, including
// counterexample traces.
package tlc

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/aburan28/tlacuilo/ast"
	"github.com/aburan28/tlacuilo/parser"
	"github.com/aburan28/tlacuilo/trace"
	"github.com/aburan28/tlacuilo/value"
)

// Message severities used by TLC's tool-mode protocol.
const (
	SeverityNone    = 0
	SeverityError   = 1
	SeverityTLCBug  = 2
	SeverityWarning = 3
	SeverityState   = 4
)

// Well-known TLC message codes (tlc2.output.EC).
const (
	CodeVersion            = 2262
	CodeModeMC             = 2187
	CodeSANYStart          = 2220
	CodeSANYEnd            = 2219
	CodeCheckingBegin      = 2185
	CodeInitComputing      = 2189
	CodeInitComputed       = 2190
	CodeInvariantViolated  = 2110
	CodeBehaviorUpToPoint  = 2121
	CodeTemporalViolated   = 2116
	CodeCounterExample     = 2264
	CodeStatePrint         = 2217
	CodeStateLoopOrStutter = 2218
	CodeProgress           = 2200
	CodeFinalStats         = 2199
	CodeSearchDepth        = 2194
	CodeFinished           = 2186
	CodeDeadlockReached    = 2114
)

// Message is one framed tool-mode message:
//
//	@!@!@STARTMSG <code>:<severity> @!@!@ body @!@!@ENDMSG <code> @!@!@
type Message struct {
	Code     int
	Severity int
	Body     string
}

var (
	startMsgRe = regexp.MustCompile(`^@!@!@STARTMSG (\d+):(\d+) @!@!@\s*$`)
	endMsgRe   = regexp.MustCompile(`^@!@!@ENDMSG (\d+) @!@!@\s*$`)
)

// ParseToolOutput reads TLC -tool output and returns the framed messages.
// Text outside message frames (such as SANY's parse log) is ignored.
// If onMsg is non-nil it is invoked for each message as it completes.
func ParseToolOutput(r io.Reader, onMsg func(Message)) ([]Message, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var msgs []Message
	var cur *Message
	var body strings.Builder
	for sc.Scan() {
		line := sc.Text()
		if m := startMsgRe.FindStringSubmatch(line); m != nil {
			code, _ := strconv.Atoi(m[1])
			sev, _ := strconv.Atoi(m[2])
			cur = &Message{Code: code, Severity: sev}
			body.Reset()
			continue
		}
		if m := endMsgRe.FindStringSubmatch(line); m != nil && cur != nil {
			cur.Body = strings.TrimRight(body.String(), "\n")
			msgs = append(msgs, *cur)
			if onMsg != nil {
				onMsg(*cur)
			}
			cur = nil
			continue
		}
		if cur != nil {
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	if err := sc.Err(); err != nil {
		return msgs, err
	}
	return msgs, nil
}

// TLCError is an error message reported by TLC.
type TLCError struct {
	Code    int
	Message string
}

func (e TLCError) Error() string { return fmt.Sprintf("TLC error %d: %s", e.Code, e.Message) }

var (
	progressRe   = regexp.MustCompile(`Progress\((\d+)\).*?: ([\d,]+) states generated.*?([\d,]+) distinct states found.*?([\d,]+) states? left on queue`)
	finalStatsRe = regexp.MustCompile(`([\d,]+) states generated, ([\d,]+) distinct states found, ([\d,]+) states? left on queue`)
	depthRe      = regexp.MustCompile(`depth of the complete state graph search is (\d+)`)
	stateHeadRe  = regexp.MustCompile(`^(\d+): <?([^>]*)>?\s*$`)
	backToRe     = regexp.MustCompile(`^(\d+): Back to state (\d+)`)
	stutterRe    = regexp.MustCompile(`^(\d+): Stuttering`)
)

func parseCount(s string) int64 {
	n, _ := strconv.ParseInt(strings.ReplaceAll(s, ",", ""), 10, 64)
	return n
}

// interpret builds Result fields from the message stream.
func (r *Result) interpret(msgs []Message) {
	for _, m := range msgs {
		switch {
		case m.Code == CodeVersion:
			r.Version = strings.TrimSpace(m.Body)
		case m.Code == CodeProgress:
			if g := progressRe.FindStringSubmatch(m.Body); g != nil {
				r.Depth, _ = strconv.Atoi(g[1])
				r.StatesGenerated = parseCount(g[2])
				r.DistinctStates = parseCount(g[3])
				r.QueueStates = parseCount(g[4])
			}
		case m.Code == CodeFinalStats:
			if g := finalStatsRe.FindStringSubmatch(m.Body); g != nil {
				r.StatesGenerated = parseCount(g[1])
				r.DistinctStates = parseCount(g[2])
				r.QueueStates = parseCount(g[3])
			}
		case m.Code == CodeSearchDepth:
			if g := depthRe.FindStringSubmatch(m.Body); g != nil {
				r.Depth, _ = strconv.Atoi(g[1])
			}
		case m.Code == CodeStatePrint && m.Severity == SeverityState:
			if s, err := parseTraceState(m.Body); err == nil {
				if r.Trace == nil {
					r.Trace = trace.New()
				}
				r.Trace.AddState(s)
			} else {
				r.Warnings = append(r.Warnings,
					fmt.Sprintf("unparseable trace state: %v", err))
			}
		case m.Code == CodeStateLoopOrStutter:
			if r.Trace == nil {
				r.Trace = trace.New()
			}
			if g := backToRe.FindStringSubmatch(m.Body); g != nil {
				n, _ := strconv.Atoi(g[2])
				r.Trace.Loop = n - 1
			} else if stutterRe.MatchString(m.Body) {
				r.Trace.Stuttering = true
			}
		case m.Severity == SeverityError:
			r.Errors = append(r.Errors, TLCError{Code: m.Code, Message: strings.TrimSpace(m.Body)})
		case m.Severity == SeverityWarning:
			r.Warnings = append(r.Warnings, strings.TrimSpace(m.Body))
		}
	}
}

// parseTraceState parses one state-print body:
//
//	2: <Next line 6, col 9 to line 7, col 56 of module Counter>
//	/\ x = 1
//	/\ hist = <<[n |-> 0, ok |-> TRUE]>>
func parseTraceState(body string) (trace.State, error) {
	s := trace.State{Vars: map[string]value.Value{}}
	body = strings.TrimSpace(body)
	nl := strings.IndexByte(body, '\n')
	head := body
	rest := ""
	if nl >= 0 {
		head, rest = body[:nl], body[nl+1:]
	}
	g := stateHeadRe.FindStringSubmatch(strings.TrimSpace(head))
	if g == nil {
		return s, fmt.Errorf("no state header in %q", head)
	}
	s.Index, _ = strconv.Atoi(g[1])
	s.Action = strings.TrimSpace(g[2])
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return s, nil
	}
	e, err := parser.ParseExpr(rest)
	if err != nil {
		return s, err
	}
	var assigns []ast.Expr
	if j, ok := e.(*ast.Junction); ok && j.Op == "/\\" {
		assigns = j.Items
	} else {
		assigns = []ast.Expr{e}
	}
	for _, a := range assigns {
		for {
			if p, ok := a.(*ast.Paren); ok {
				a = p.X
				continue
			}
			break
		}
		b, ok := a.(*ast.Binary)
		if !ok || b.Op != "=" {
			return s, fmt.Errorf("state line is not an assignment: %s", ast.ExprString(a))
		}
		id, ok := b.L.(*ast.Ident)
		if !ok {
			return s, fmt.Errorf("assignment target is not a variable: %s", ast.ExprString(b.L))
		}
		v, err := value.FromExpr(b.R)
		if err != nil {
			return s, fmt.Errorf("variable %s: %w", id.Name, err)
		}
		s.Vars[id.Name] = v
	}
	return s, nil
}
