package constants

// Judge result status codes
const (
	OJ_WT0 = 0  // Waiting in queue
	OJ_WT1 = 1  // Rejudge waiting
	OJ_CI  = 2  // Compiling
	OJ_RI  = 3  // Running
	OJ_AC  = 4  // Accepted
	OJ_PE  = 5  // Presentation Error
	OJ_WA  = 6  // Wrong Answer
	OJ_TL  = 7  // Time Limit Exceeded
	OJ_ML  = 8  // Memory Limit Exceeded
	OJ_OL  = 9  // Output Limit Exceeded
	OJ_RE  = 10 // Runtime Error
	OJ_CE  = 11 // Compile Error
	OJ_CO  = 12 // Compile Complete
	OJ_TR  = 13 // Test Run Complete
	OJ_MC  = 14 // Manual Check Required
	OJ_SE  = 99 // System Error
)

// GetOJResultName returns the string representation of judge status
func GetOJResultName(status int) string {
	var names = []string{"WT0", "WT1", "CI", "RI", "AC", "PE", "WA", "TL", "ML", "OL", "RE", "CE", "CO", "TR", "MC"}
	if status == OJ_SE {
		return "SE"
	}
	if status < 0 || status >= len(names) {
		return "OT"
	}
	return names[status]
}

// Special judge modes
const (
	OJ_SPJ_MODE_NONE    = 0 // No special judge
	OJ_SPJ_MODE_SPJ     = 1 // Special judge program
	OJ_SPJ_MODE_RAWTEXT = 2 // Raw text comparison judge
)

// Special judge program variants (used when OJ_SPJ_MODE_SPJ is set)
const (
	OJ_SPJ_PROGRAM_SPJ = 1 // hustoj style: infile outfile userfile
	OJ_SPJ_PROGRAM_TPJ = 2 // testlib style: infile userfile outfile
	OJ_SPJ_PROGRAM_UPJ = 3 // hustoj style with score: infile outfile userfile, return 0-100
)
