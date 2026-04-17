package gitlab

// MinSupportedVersion is the earliest glab release the adapter has been
// validated against. Doctor (nc-234) uses this to warn when the installed
// version is older.
const MinSupportedVersion = "1.43.0"

// TestedAgainst is the exact glab release used to capture the testdata
// goldens. Bump deliberately when a JSON-shape change forces re-capture.
const TestedAgainst = "1.43.2"
