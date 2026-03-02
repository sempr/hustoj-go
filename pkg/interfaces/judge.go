package interfaces

import (
	"github.com/sempr/hustoj-go/pkg/models"
)

// JudgeClient defines the interface for judging submissions
type JudgeClient interface {
	Run() error
	Close() error
}

// JobFetcher defines the interface for fetching judge jobs
type JobFetcher interface {
	GetJobs(maxJobs int) ([]int, error)
	CheckOut(solutionID int, result int) (bool, error)
	Close() error
}

// Database defines the interface for database operations
type Database interface {
	GetSolution(solutionID int) (*Solution, error)
	GetProblem(problemID int) (*Problem, error)
	GetSolutionSource(solutionID int) (string, error)
	UpdateSolution(solutionID, result, timeUsed, memoryUsed int, passRate float64) error
	UpdateUserStats(userID string) error
	UpdateProblemStats(problemID, contestID int) error
	AddCompileError(solutionID int, message string) error
	AddRuntimeInfo(solutionID int, details string) error
	Close() error
}

// LanguageManager defines the interface for language configuration
type LanguageManager interface {
	GetLanguageBasic(langID int) (LangBasic, error)
	GetLanguageConfig(langID int) (*LangConfig, error)
	GetAllLanguages() map[int]LangBasic
}

// Data types
type Solution struct {
	ID        int
	ProblemID int
	UserID    string
	Language  int
	ContestID int
}

type Problem struct {
	ID        int
	TimeLimit float64
	MemLimit  int
	SPJ       int
}

type LangBasic struct {
	Name   string `toml:"name"`
	ID     int    `toml:"id"`
	Suffix string `toml:"suffix"`
}

type LangConfig struct {
	Name string  `toml:"name"`
	Fs   FsInfo  `toml:"fs"`
	Cmd  CmdInfo `toml:"cmd"`
}

type FsInfo struct {
	Base    string `toml:"base"`
	Workdir string `toml:"workdir"`
}

type CmdInfo struct {
	Compile string   `toml:"compile"`
	Run     string   `toml:"run"`
	Ver     string   `toml:"ver"`
	Env     []string `toml:"env"`
}

// SandboxExecutor defines interface for executing code in sandbox
type SandboxExecutor interface {
	Compile(rootfs string, cmd string, env []string, solutionID int) (*models.SandboxOutput, error)
	Run(rootfs string, cmd string, env []string, solutionID int, stdin, stdout string, timeLimit, memoryLimit int) (*models.SandboxOutput, error)
}
