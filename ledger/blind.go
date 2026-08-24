package ledger

import (
	"errors"

	"github.com/olivepress/fruit-intake-gate/task"
)

// ErrGenerationMismatch is returned when an operation targets a generation
// other than the current task generation.
var ErrGenerationMismatch = errors.New("ledger: generation mismatch")

// Reveal links a blind sample to its crate seal. The reveal must match the
// current task generation and the crate must already be confirmed by the
// receiving gate; a sample can only be revealed once.
func (s *BlindSample) Reveal(crateSeal string, gen task.Generation, crateConfirmed bool) error {
	if s.Generation != gen {
		return ErrGenerationMismatch
	}
	if !crateConfirmed {
		return ErrCrateNotConfirmed
	}
	if s.RevealedCrateSeal != "" {
		return ErrAlreadyRevealed
	}
	if crateSeal == "" {
		return ErrCrateSealUnknown
	}
	s.RevealedCrateSeal = crateSeal
	return nil
}

// Revealed reports whether the blind sample mapping has been revealed.
func (s *BlindSample) Revealed() bool {
	return s.RevealedCrateSeal != ""
}

// BlindSamples is a set of blind samples for one task.
type BlindSamples struct {
	TaskID  task.TaskID
	Samples map[string]*BlindSample
}

// Reveal maps a blind code to a crate seal, validating generation and split
// confirmation.
func (b *BlindSamples) Reveal(blindCode, crateSeal string, gen task.Generation, crateConfirmed bool) error {
	s, ok := b.Samples[blindCode]
	if !ok {
		return ErrCrateSealUnknown
	}
	return s.Reveal(crateSeal, gen, crateConfirmed)
}
