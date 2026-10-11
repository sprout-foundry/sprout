//go:build !js

package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// plistEnvNames returns the variable names in a launchd plist's top-level
// EnvironmentVariables dict, in file order. Values are never read out.
func plistEnvNames(data []byte) ([]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var (
		names      []string
		depth      int  // current <dict> nesting depth
		envDepth   int  // depth of the EnvironmentVariables dict; 0 outside it
		pendingEnv bool // the last top-level key was EnvironmentVariables
		inKey      bool
		key        strings.Builder
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return names, nil
		}
		if err != nil {
			// The decoder's message can quote plist text, which may be a
			// secret value; report only the position.
			var syntaxErr *xml.SyntaxError
			if errors.As(err, &syntaxErr) {
				return nil, fmt.Errorf("plist is not well-formed XML (line %d)", syntaxErr.Line)
			}
			return nil, errors.New("plist is not well-formed XML")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				depth++
				if pendingEnv && depth == 2 {
					envDepth = depth
				}
				pendingEnv = false
			case "key":
				inKey = true
				key.Reset()
			default:
				pendingEnv = false
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "dict":
				if depth == envDepth {
					envDepth = 0
				}
				depth--
			case "key":
				inKey = false
				switch {
				case envDepth != 0 && depth == envDepth:
					names = append(names, key.String())
				case depth == 1:
					pendingEnv = key.String() == "EnvironmentVariables"
				}
			}
		case xml.CharData:
			if inKey {
				key.Write(t)
			}
		}
	}
}
