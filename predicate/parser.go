package predicate

import (
	"fmt"
	"strings"

	"github.com/influxdata/influxdb"
	"github.com/influxdata/influxql"
)

// a fixed buffer ring
type buffer [3]struct {
	tok influxql.Token // last read token
	pos influxql.Pos   // last read pos
	lit string         // last read literal
}

// parser of the predicate will connvert
// such a statement `(a = "a" or b!="b") and c ! =~/efg/`
// to the predicate node
type parser struct {
	sc        *influxql.Scanner
	i         int // buffer index
	n         int // buffer size
	openParen int
	buf       buffer
}

// scan returns the next token from the underlying scanner.
// If a token has been unscanned then read that instead.
func (p *parser) scan() (tok influxql.Token, pos influxql.Pos, lit string) {
	// If we have a token on the buffer, then return it.
	if p.n > 0 {
		p.n--
		return p.curr()
	}

	// Move buffer position forward and save the token.
	p.i = (p.i + 1) % len(p.buf)
	buf := &p.buf[p.i]
	buf.tok, buf.pos, buf.lit = p.sc.Scan()

	return p.curr()
}

func (p *parser) unscan() {
	p.n++
}

// curr returns the last read token.
func (p *parser) curr() (tok influxql.Token, pos influxql.Pos, lit string) {
	buf := &p.buf[(p.i-p.n+len(p.buf))%len(p.buf)]
	return buf.tok, buf.pos, buf.lit
}

// scanIgnoreWhitespace scans the next non-whitespace token.
func (p *parser) scanIgnoreWhitespace() (tok influxql.Token, pos influxql.Pos, lit string) {
	tok, pos, lit = p.scan()
	if tok == influxql.WS {
		tok, pos, lit = p.scan()
	}
	return
}

// Parse the predicate statement.
func Parse(sts string) (n Node, err error) {
	if sts == "" {
		return nil, nil
	}
	p := new(parser)
	p.sc = influxql.NewScanner(strings.NewReader(sts))
	return p.parseLogicalNode()
}

func (p *parser) parseLogicalNode() (Node, error) {
	n := &LogicalNode{
		Children: make([]Node, 0),
	}
	for {
		tok, pos, _ := p.scanIgnoreWhitespace()
		switch tok {
		case influxql.NUMBER:
			fallthrough
		case influxql.INTEGER:
			fallthrough
		case influxql.NAME:
			fallthrough
		case influxql.IDENT:
			p.unscan()
			tr, err := p.parseTagRuleNode()
			if err != nil {
				return *n, err
			}
			n.Children = append(n.Children, tr)
		case influxql.AND:
			n.Operator = LogicalAnd
		case influxql.OR:
			return *n, &influxdb.Error{
				Code: influxdb.EInvalid,
				Msg:  fmt.Sprintf("the logical operator OR is not supported yet at position %d", pos.Char),
			}
		case influxql.LPAREN:
			p.openParen++
			currParen := p.openParen
			n1, err := p.parseLogicalNode()
			if err != nil {
				return *n, err
			}
			if p.openParen != currParen-1 {
				return *n, &influxdb.Error{
					Code: influxdb.EInvalid,
					Msg:  fmt.Sprintf("extra ( seen"),
				}
			}
			n.Children = append(n.Children, n1)
		case influxql.RPAREN:
			p.openParen--
			fallthrough
		case influxql.EOF:
			if p.openParen < 0 {
				return *n, &influxdb.Error{
					Code: influxdb.EInvalid,
					Msg:  fmt.Sprintf("extra ) seen"),
				}
			}
			if len(n.Children) == 1 {
				return n.Children[0], nil
			}
			return *n, nil
		default:
			return *n, &influxdb.Error{
				Code: influxdb.EInvalid,
				Msg:  fmt.Sprintf("bad logical expression, at position %d", pos.Char),
			}
		}
	}
}

func (p *parser) parseTagRuleNode() (TagRuleNode, error) {
	n := new(TagRuleNode)
	// scan the key
	tok, pos, lit := p.scanIgnoreWhitespace()
	switch tok {
	case influxql.IDENT:
		n.Key = lit
	case influxql.NAME:
		n.Key = "name"
	default:
		return *n, &influxdb.Error{
			Code: influxdb.EInvalid,
			Msg:  fmt.Sprintf("bad tag key, at position %d", pos.Char),
		}
	}

	tok, pos, _ = p.scanIgnoreWhitespace()
	switch tok {
	case influxql.EQ:
		n.Operator = influxdb.Equal
		goto scanRegularTagValue
	case influxql.NEQ:
		fallthrough
	case influxql.EQREGEX:
		fallthrough
	case influxql.NEQREGEX:
		return *n, &influxdb.Error{
			Code: influxdb.EInvalid,
			Msg:  fmt.Sprintf("operator: %q at position: %d is not supported yet", tok.String(), pos.Char),
		}
	default:
		return *n, &influxdb.Error{
			Code: influxdb.EInvalid,
			Msg:  fmt.Sprintf("invalid operator %q at position: %d", tok.String(), pos.Char),
		}
	}
	// scan the value
scanRegularTagValue:
	tok, pos, lit = p.scanIgnoreWhitespace()
	switch tok {
	case influxql.IDENT:
		fallthrough
	case influxql.NUMBER:
		fallthrough
	case influxql.INTEGER:
		n.Value = lit
		return *n, nil
	case influxql.TRUE:
		n.Value = "true"
		return *n, nil
	case influxql.FALSE:
		n.Value = "false"
		return *n, nil
	default:
		return *n, &influxdb.Error{
			Code: influxdb.EInvalid,
			Msg:  fmt.Sprintf("bad tag value: %q, at position %d", lit, pos.Char),
		}
	}
}