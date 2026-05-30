package misc

import (
  "strings"
)

// EggState holds global easter egg state
type EggState struct {
  Enabled bool
  Message string
  Active  bool
}

var GlobalEgg EggState

// CheckEggKey detects shibboleth input.
// ø = original operator marker; £ = UK keyboard shortcut.
func CheckEggKey(r rune) bool {
  return r == 'ø' || r == '£'
}

// TriggerEgg activates hidden state
func TriggerEgg() {
  GlobalEgg.Enabled = true
  GlobalEgg.Active = true

  GlobalEgg.Message = strings.Join([]string{
    "You get used to it.",
    "I don't even see the code...",
    "all I see is blond, brunette, redhead...",
    "",
    "Jeg har det som blommen i et æg!",
  }, "\n")
}

// ResetEgg clears state (optional future use)
func ResetEgg() {
  GlobalEgg = EggState{}
}
