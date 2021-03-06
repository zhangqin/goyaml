//
//	scanner/scanner.go
//	goyaml
//
//	Created by Ross Light on 2010-06-24.
//

/*
	The scanner package is responsible for parsing a YAML document and
	transforming it into a sequence of events.  This corresponds to the parsing
	stage in the YAML 1.2 specification.
*/
package scanner

import (
	"container/list"
	"errors"
	"fmt"
	"io"
	"code.google.com/p/goyaml/token"
)

type simpleKey struct {
	Possible    bool
	Required    bool
	Pos         token.Position
	TokenNumber uint
}

// Error is the error type returned by a Scanner. It provides the position of the error.
type Error struct {
	error
	Pos token.Position
}

func (err Error) String() string {
	return fmt.Sprintf("%v: %v", err.Pos, err.Error())
}

// A Scanner generates a sequence of lexical tokens from a reader containing YAML data.
type Scanner struct {
	reader      *reader
	tokenQueue  *list.List
	parsedCount uint
	started     bool
	ended       bool

	indent      int
	indentStack []int

	simpleKeyAllowed bool
	simpleKeyStack   []*simpleKey

	flowLevel uint
}

// New creates a new Scanner from a reader.
func New(r io.Reader) (s *Scanner) {
	s = new(Scanner)
	s.reader = newReader(r)
	s.tokenQueue = list.New()
	return s
}

// GetPosition returns the position of the first unread byte from the
// underlying reader.
//
// This does not necessarily correspond to the starting position of the token
// that will be returned next by Scan, nor does it even correspond to the
// position in the reader (more bytes may have actually been read).  The
// Scanner has to do some look-ahead to do its job.
func (s *Scanner) GetPosition() token.Position { return s.reader.Pos }

// Scan returns the next token in the stream.  If the stream has already ended,
// then this method will return nil, nil.
func (s *Scanner) Scan() (result Token, err error) {
	if s.tokenQueue.Len() == 0 && s.ended {
		return
	}
	if err = s.prepare(); err != nil {
		return
	}
	elem := s.tokenQueue.Front()
	result = elem.Value.(Token)
	s.tokenQueue.Remove(elem)
	s.parsedCount++
	return
}

func (s *Scanner) wrapError(origErr error) (err Error) {
	var ok bool
	if err, ok = origErr.(Error); ok {
		return
	}
	return Error{origErr, s.reader.Pos}
}

// prepare ensures that there is a token to return.  This will look ahead a few
// tokens in some cases to ensure that the tokens are logical.
func (s *Scanner) prepare() (err error) {
	for {
		needMore := false
		if s.tokenQueue.Len() == 0 {
			needMore = true
		} else {
			if err = s.removeStaleSimpleKeys(); err != nil {
				err = s.wrapError(err)
				return
			}
			for _, skey := range s.simpleKeyStack {
				if skey.Possible && skey.TokenNumber == s.parsedCount {
					needMore = true
					break
				}
			}
		}
		// Are we finished?
		if !needMore {
			break
		}
		// Fetch next token
		err = s.fetch()
		if err != nil {
			err = s.wrapError(err)
			return
		}
	}
	return nil
}

// fetch adds the next token in the stream to the queue.
func (s *Scanner) fetch() (err error) {
	if !s.started {
		s.streamStart()
		return
	}

	if err = s.scanToNextToken(); err != nil {
		return
	}
	if err = s.removeStaleSimpleKeys(); err != nil {
		return
	}

	s.unrollIndent(s.GetPosition().Column)

	if err = s.reader.Cache(4); err != nil {
		return
	}

	if s.reader.Len() == 0 {
		// No characters left? End the stream.
		return s.streamEnd()
	}

	switch {
	case s.GetPosition().Column == 1 && s.reader.Check(0, "%"):
		return s.fetchDirective()
	case s.GetPosition().Column == 1 && s.reader.Check(0, "---") && s.reader.CheckBlank(3):
		return s.fetchDocumentIndicator(token.DOCUMENT_START)
	case s.GetPosition().Column == 1 && s.reader.Check(0, "...") && s.reader.CheckBlank(3):
		return s.fetchDocumentIndicator(token.DOCUMENT_END)
	case s.reader.Check(0, "["):
		return s.fetchFlowCollectionStart(token.FLOW_SEQUENCE_START)
	case s.reader.Check(0, "{"):
		return s.fetchFlowCollectionStart(token.FLOW_MAPPING_START)
	case s.reader.Check(0, "]"):
		return s.fetchFlowCollectionEnd(token.FLOW_SEQUENCE_END)
	case s.reader.Check(0, "}"):
		return s.fetchFlowCollectionEnd(token.FLOW_MAPPING_END)
	case s.reader.Check(0, ","):
		return s.fetchFlowEntry()
	case s.reader.Check(0, "-") && s.reader.CheckBlank(1):
		return s.fetchBlockEntry()
	case s.reader.Check(0, "?") && (s.flowLevel > 0 || s.reader.CheckBlank(1)):
		return s.fetchKey()
	case s.reader.Check(0, ":") && (s.flowLevel > 0 || s.reader.CheckBlank(1)):
		return s.fetchValue()
	case s.reader.Check(0, "*"):
		return s.fetchAnchor(token.ALIAS)
	case s.reader.Check(0, "&"):
		return s.fetchAnchor(token.ANCHOR)
	case s.reader.Check(0, "!"):
		return s.fetchTag()
	case s.reader.Check(0, "|") && s.flowLevel == 0:
		return s.fetchBlockScalar(LiteralScalarStyle)
	case s.reader.Check(0, ">") && s.flowLevel == 0:
		return s.fetchBlockScalar(FoldedScalarStyle)
	case s.reader.Check(0, "'"):
		return s.fetchFlowScalar(SingleQuotedScalarStyle)
	case s.reader.Check(0, "\""):
		return s.fetchFlowScalar(DoubleQuotedScalarStyle)
	case !(s.reader.CheckBlank(0) || s.reader.CheckAny(0, "-?:,[]{}#&*!|>'\"%@`")) || (s.reader.Check(0, "-") && !s.reader.CheckSpace(1)) || (s.flowLevel == 0 && s.reader.CheckAny(0, "?:") && !s.reader.CheckBlank(1)):
		return s.fetchPlainScalar()
	default:
		err = errors.New(fmt.Sprintf("Unrecognized token: %c", s.reader.Bytes()[0]))
	}
	return
}

func (s *Scanner) addToken(t Token) {
	s.tokenQueue.PushBack(t)
}

func (s *Scanner) insertToken(num uint, t Token) {
	queueIndex := num - s.parsedCount
	if queueIndex < uint(s.tokenQueue.Len()) {
		elem := s.tokenQueue.Front()
		for i := uint(0); i < queueIndex; i++ {
			elem = elem.Next()
		}
		s.tokenQueue.InsertBefore(t, elem)
	} else {
		s.tokenQueue.PushBack(t)
	}
}

func (s *Scanner) removeStaleSimpleKeys() (err error) {
	for _, key := range s.simpleKeyStack {
		// A simple key is:
		// - limited to a single line
		// - shorter than 1024 characters
		if key.Possible && (key.Pos.Line < s.GetPosition().Line || key.Pos.Index+1024 < s.GetPosition().Index) {
			if key.Required {
				return errors.New("Could not find expected ':'")
			}
			key.Possible = false
		}
	}
	return
}

func (s *Scanner) saveSimpleKey() (err error) {
	required := s.flowLevel == 0 && s.indent == s.GetPosition().Column
	if s.simpleKeyAllowed {
		key := simpleKey{
			Possible:    true,
			Required:    required,
			Pos:         s.GetPosition(),
			TokenNumber: s.parsedCount + uint(s.tokenQueue.Len()),
		}
		if err = s.removeSimpleKey(); err != nil {
			return
		}
		s.simpleKeyStack[len(s.simpleKeyStack)-1] = &key
	}
	return nil
}

func (s *Scanner) removeSimpleKey() (err error) {
	key := s.simpleKeyStack[len(s.simpleKeyStack)-1]
	if key.Possible && key.Required {
		return errors.New("Could not find expected ':'")
	}
	key.Possible = false
	return nil
}

func (s *Scanner) increaseFlowLevel() {
	s.simpleKeyStack = append(s.simpleKeyStack, new(simpleKey))
	s.flowLevel++
}

func (s *Scanner) decreaseFlowLevel() {
	if s.flowLevel > 0 {
		s.flowLevel--
		s.simpleKeyStack[len(s.simpleKeyStack)-1] = nil
		s.simpleKeyStack = s.simpleKeyStack[:len(s.simpleKeyStack)-1]
	}
}

func (s *Scanner) rollIndent(column, tokenNumber int, kind token.Token, pos token.Position) {
	if s.flowLevel > 0 {
		return
	}

	if s.indent < column {
		// Push the current indentation level to the stack and set the new
		// indentation level.
		s.indentStack = append(s.indentStack, s.indent)
		s.indent = column
		tok := BasicToken{
			Kind:  kind,
			Start: pos,
			End:   pos,
		}
		if tokenNumber == -1 {
			s.addToken(tok)
		} else {
			s.insertToken(uint(tokenNumber), tok)
		}
	}
}

func (s *Scanner) unrollIndent(column int) {
	// In flow context, do nothing.
	if s.flowLevel > 0 {
		return
	}

	for s.indent > column {
		s.addToken(BasicToken{
			Kind:  token.BLOCK_END,
			Start: s.GetPosition(),
			End:   s.GetPosition(),
		})
		s.indent = s.indentStack[len(s.indentStack)-1]
		s.indentStack = s.indentStack[:len(s.indentStack)-1]
	}
}

func (s *Scanner) streamStart() {
	s.indent = 0
	s.simpleKeyStack = append(s.simpleKeyStack, new(simpleKey))
	s.simpleKeyAllowed = true
	s.started = true
	s.addToken(BasicToken{
		Kind:  token.STREAM_START,
		Start: s.GetPosition(),
		End:   s.GetPosition(),
	})
}

func (s *Scanner) streamEnd() (err error) {
	s.ended = true
	// Force new line
	if s.GetPosition().Column != 1 {
		s.reader.Pos.Column = 1
		s.reader.Pos.Line++
	}
	// Reset indentation level
	s.unrollIndent(0)
	// Reset simple keys
	if err = s.removeSimpleKey(); err != nil {
		return
	}
	s.simpleKeyAllowed = false
	// End the stream
	s.addToken(BasicToken{
		Kind:  token.STREAM_END,
		Start: s.GetPosition(),
		End:   s.GetPosition(),
	})
	return nil
}

func (s *Scanner) fetchDirective() (err error) {
	// Reset indentation level
	s.unrollIndent(0)
	// Reset simple keys
	if err = s.removeSimpleKey(); err != nil {
		return
	}
	s.simpleKeyAllowed = false
	// Create token
	var tok Token
	if tok, err = s.scanDirective(); err != nil {
		return
	}
	s.addToken(tok)
	return
}

func (s *Scanner) fetchDocumentIndicator(kind token.Token) (err error) {
	// Reset indentation level
	s.unrollIndent(0)
	// Reset simple keys
	if err = s.removeSimpleKey(); err != nil {
		return
	}
	s.simpleKeyAllowed = false
	// Consume the token
	startPos := s.GetPosition()
	s.reader.Next(3)
	endPos := s.GetPosition()
	// Create the scanner token
	s.addToken(BasicToken{
		Kind:  kind,
		Start: startPos,
		End:   endPos,
	})
	return
}

func (s *Scanner) fetchFlowCollectionStart(kind token.Token) (err error) {
	// The indicators '[' and '{' may start a simple key
	if err = s.saveSimpleKey(); err != nil {
		return
	}
	s.increaseFlowLevel()
	// A simple key may follow the indicators '[' and '{'
	s.simpleKeyAllowed = true
	// Consume the token
	startPos := s.GetPosition()
	s.reader.Next(1)
	s.addToken(BasicToken{
		Kind:  kind,
		Start: startPos,
		End:   s.GetPosition(),
	})
	return
}

func (s *Scanner) fetchFlowCollectionEnd(kind token.Token) (err error) {
	// Reset any potential simple key on the current flow level
	if err = s.removeSimpleKey(); err != nil {
		return
	}
	s.decreaseFlowLevel()
	// No simple keys after the indicators ']' and '}'
	s.simpleKeyAllowed = false
	// Consume the token
	startPos := s.GetPosition()
	s.reader.Next(1)
	s.addToken(BasicToken{
		Kind:  kind,
		Start: startPos,
		End:   s.GetPosition(),
	})
	return
}

func (s *Scanner) fetchFlowEntry() (err error) {
	// Reset any potential simple keys on the current flow level
	if err = s.removeSimpleKey(); err != nil {
		return
	}
	// Simple keys are allowed after ','
	s.simpleKeyAllowed = true
	// Consume the token
	startPos := s.GetPosition()
	s.reader.Next(1)
	s.addToken(BasicToken{
		Kind:  token.FLOW_ENTRY,
		Start: startPos,
		End:   s.GetPosition(),
	})
	return
}

func (s *Scanner) fetchBlockEntry() (err error) {
	// Are we in the block context?
	if s.flowLevel == 0 {
		// Are we allowed to start a new entry?
		if !s.simpleKeyAllowed {
			err = errors.New("Block sequence entries are not allowed in this context")
			return
		}
		// Add the BLOCK_SEQUENCE_START token, if needed
		s.rollIndent(s.GetPosition().Column, -1, token.BLOCK_SEQUENCE_START, s.GetPosition())
	}
	// It is an error for the '-' indicator to occur in the flow context, but we let
	// the Parser detect and report about it because the Parser is able to point to
	// the context.

	// Reset any potential simple keys on the current flow level
	if err = s.removeSimpleKey(); err != nil {
		return
	}

	// Simple keys are allowed after '-'
	s.simpleKeyAllowed = true

	// Consume the token
	startPos := s.GetPosition()
	s.reader.Next(1)
	s.addToken(BasicToken{
		Kind:  token.BLOCK_ENTRY,
		Start: startPos,
		End:   s.GetPosition(),
	})
	return
}

func (s *Scanner) fetchKey() (err error) {
	// In block context, additional checks are required
	if s.flowLevel == 0 {
		// Are we allowed to start a new key?
		if s.simpleKeyAllowed {
			err = errors.New("Mapping keys are not allowed in this context")
			return
		}
		// Add BLOCK_MAPPING_START token if needed
		s.rollIndent(s.GetPosition().Column, -1, token.BLOCK_MAPPING_START, s.GetPosition())
	}
	// Reset any potential simple keys on the current flow level
	if err = s.removeSimpleKey(); err != nil {
		return
	}
	// Simple keys are allowed after '?' in the block context
	s.simpleKeyAllowed = (s.flowLevel > 0)
	// Consume the token
	startPos := s.GetPosition()
	s.reader.Next(1)
	s.addToken(BasicToken{
		Kind:  token.KEY,
		Start: startPos,
		End:   s.GetPosition(),
	})
	return
}

func (s *Scanner) fetchValue() (err error) {
	skey := s.simpleKeyStack[len(s.simpleKeyStack)-1]
	// Have we found a simple key?
	if skey.Possible {
		// Create the KEY token and insert it into the queue
		tok := BasicToken{token.KEY, skey.Pos, skey.Pos}
		s.insertToken(skey.TokenNumber, tok)
		// In the block context, we may need to add the BLOCK_MAPPING_START token
		s.rollIndent(skey.Pos.Column, int(skey.TokenNumber), token.BLOCK_MAPPING_START, skey.Pos)
		// Remove the simple key
		skey.Possible = false
		// A simple key cannot follow another simple key
		s.simpleKeyAllowed = false
	} else {
		// The ':' indicator follows a complex key
		// In the block context, extra checks are required
		if s.flowLevel == 0 {
			// Are we allowed to start a complex value?
			if !s.simpleKeyAllowed {
				err = errors.New("Mapping values are not allowed in this context")
				return
			}
			// Add the BLOCK_MAPPING_START token, if needed
			s.rollIndent(s.GetPosition().Column, -1, token.BLOCK_MAPPING_START, s.GetPosition())
		}
		// Simple keys after ':' are allowed in the block context
		s.simpleKeyAllowed = (s.flowLevel > 0)
	}
	// Consume the token
	startPos := s.GetPosition()
	s.reader.Next(1)
	s.addToken(BasicToken{
		Kind:  token.VALUE,
		Start: startPos,
		End:   s.GetPosition(),
	})
	return nil
	return
}

func (s *Scanner) fetchAnchor(kind token.Token) (err error) {
	// An anchor/alias could be a simple key
	if err = s.saveSimpleKey(); err != nil {
		return
	}
	// A simple key cannot follow an anchor/alias
	s.simpleKeyAllowed = false
	// Consume the token
	tok, err := s.scanAnchor(kind)
	if err == nil {
		s.addToken(tok)
	}
	return
}

func (s *Scanner) fetchTag() (err error) {
	if err = s.saveSimpleKey(); err != nil {
		return
	}
	s.simpleKeyAllowed = false
	tok, err := s.scanTag()
	if err == nil {
		s.addToken(tok)
	}
	return
}

func (s *Scanner) fetchBlockScalar(style int) (err error) {
	if err = s.removeSimpleKey(); err != nil {
		return err
	}
	s.simpleKeyAllowed = true
	tok, err := s.scanBlockScalar(style)
	if err == nil {
		s.addToken(tok)
	}
	return
}

func (s *Scanner) fetchFlowScalar(style int) (err error) {
	if err = s.saveSimpleKey(); err != nil {
		return
	}
	s.simpleKeyAllowed = false
	tok, err := s.scanFlowScalar(style)
	if err == nil {
		s.addToken(tok)
	}
	return
}

func (s *Scanner) fetchPlainScalar() (err error) {
	// A plain scalar could be a simple key
	if err = s.saveSimpleKey(); err != nil {
		return
	}
	// A simple key cannot follow a flow scalar
	s.simpleKeyAllowed = false
	// Scan in scalar
	tok, err := s.scanPlainScalar()
	if err == nil {
		s.addToken(tok)
	}
	return
}
