package enforcement

import (
	"bytes"
	"encoding/json"
)

// rejectDuplicateJSONKeys walks one raw JSON value before struct decoding.
// encoding/json otherwise keeps the last occurrence of a repeated object key,
// making the order of attacker-controlled predicate results change a decision.
func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := checkJSONValue(decoder); err != nil {
		return errMalformed
	}
	return nil
}

// checkJSONValue consumes one value and rejects repeated keys in every nested
// object. It returns only the package's static error sentinel, never source
// bytes or a decoder diagnostic that could reflect secret-bearing input.
func checkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return errMalformed
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return errMalformed
			}
			key, ok := keyToken.(string)
			if !ok {
				return errMalformed
			}
			if _, duplicate := seen[key]; duplicate {
				return errMalformed
			}
			seen[key] = struct{}{}
			if err := checkJSONValue(decoder); err != nil {
				return errMalformed
			}
		}
		return consumeJSONDelimiter(decoder, '}')
	case '[':
		for decoder.More() {
			if err := checkJSONValue(decoder); err != nil {
				return errMalformed
			}
		}
		return consumeJSONDelimiter(decoder, ']')
	default:
		return errMalformed
	}
}

func consumeJSONDelimiter(decoder *json.Decoder, expected json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return errMalformed
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != expected {
		return errMalformed
	}
	return nil
}
