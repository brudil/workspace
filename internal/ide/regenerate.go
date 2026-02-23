package ide

import "errors"

// Regenerate updates all detected IDE workspace files from board state.
// Only mutates files that already exist.
func Regenerate(root string, boarded map[string][]string, displayNames map[string]string, org string) error {
	var errs []error
	if err := GenerateVSCode(root, boarded, displayNames); err != nil {
		errs = append(errs, err)
	}
	if err := GenerateIDEA(root, boarded, org); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
