/*
 * Copyright (c) 2021-present Sigma-Soft, Ltd.
 * @author: Nikolay Nikitin
 */

package appdef

import (
	"fmt"
)

// Empty name
const NullName = ""

// Any name
const AnyName = "ANY"

// Returns is string is valid identifier and error if not
func ValidIdent(ident string) (bool, error) {
	if len(ident) < 1 {
		return false, ErrNameMissed
	}

	if len(ident) > MaxIdentLen {
		return false, fmt.Errorf("ident too long: %w", ErrInvalidName)
	}

	const (
		char_a    rune = 97
		char_A    rune = 65
		char_z    rune = 122
		char_Z    rune = 90
		char_0    rune = 48
		char_9    rune = 57
		char__    rune = 95
		char_Buck rune = 36
	)

	digit := func(r rune) bool { return (char_0 <= r) && (r <= char_9) }

	letter := func(r rune) bool { return ((char_a <= r) && (r <= char_z)) || ((char_A <= r) && (r <= char_Z)) }

	underScore := func(r rune) bool { return r == char__ }

	buck := func(r rune) bool { return r == char_Buck }

	for p, c := range ident {
		if !letter(c) && !underScore(c) && !buck(c) {
			if (p == 0) || !digit(c) {
				return false, fmt.Errorf("name char «%c» at pos %d is not valid: %w", c, p, ErrInvalidName)
			}
		}
	}

	return true, nil
}
