package client

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog" // 导入 slog
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/pelletier/go-toml/v2"
	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/models"
	"golang.org/x/sys/unix"
)

// 配置变量 (简化 C++ 中的全局变量)
var (
	dbHost         string
	dbPort         int
	dbUser         string
	dbPass         string
	dbName         string
	ojHome         string
	tbName         string = "solution"  // 默认表名
	httpJudgerName string = "go_judger" // 充当 judger 字段
)

type langBasic struct {
	Name   string `toml:"name"`
	ID     int    `toml:"id"`
	Suffix string `toml:"suffix"`
}

type langConfigs struct {
	Lang []langBasic `toml:"lang"`
}

type langDetails struct {
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

var langMaps map[int]langBasic
var langDetail langDetails
var rsolutionID int

func getLangMaps(path string) map[int]langBasic {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 无法读取文件: %v\n", err)
		os.Exit(1)
	}

	// 声明一个 Config 变量，用于存储解析后的数据
	var tempConfig langConfigs

	// 使用 toml.Unmarshal 将文件内容解析到 config 变量中
	err = toml.Unmarshal(data, &tempConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 无法解析 TOML: %v\n", err)
		os.Exit(1)
	}

	langMap := make(map[int]langBasic)

	// 4. 遍历解析出的切片 (tempConfig.Lang)，将其填充到 Map 中
	for _, lang := range tempConfig.Lang {
		langMap[lang.ID] = lang
	}
	return langMap
}

func getLangDetails(lang int) (langDetails, error) {
	data, err := os.ReadFile(filepath.Join(ojHome, "etc", "langs", fmt.Sprintf("%d.lang.toml", lang)))
	if err != nil {
		return langDetails{}, fmt.Errorf("读取语言配置文件失败: %w", err)
	}
	var tempConfig langDetails
	err = toml.Unmarshal(data, &tempConfig)
	if err != nil {
		return langDetails{}, fmt.Errorf("解析语言配置文件失败: %w", err)
	}
	return tempConfig, nil
}

// initJudgeConf (使用 slog)
// 从 /home/judge/etc/judge.conf 读取配置
func initJudgeConf(homePath string) {
	ojHome = homePath

	// 1. 设置默认值
	dbHost = "127.0.0.1"
	dbPort = 3306
	dbUser = "root"
	dbPass = "password" // 默认值，应在配置文件中覆盖
	dbName = "hustoj"

	slog.Info("正在加载配置...")

	// 2. 构造配置文件路径
	confPath := filepath.Join(ojHome, "etc", "judge.conf")
	slog.Info("尝试读取配置文件", "path", confPath)

	// 3. 打开并解析文件
	file, err := os.Open(confPath)
	if err != nil {
		slog.Warn("配置文件未找到，将使用默认值", "path", confPath)
		// 记录正在使用的默认值
		slog.Info("  使用默认值", "OJ_HOME", ojHome)
		slog.Info("  使用默认值", "DB_HOST", dbHost)
		slog.Info("  使用默认值", "DB_PORT", dbPort)
		slog.Info("  使用默认值", "DB_NAME", dbName)
		return
	}
	defer file.Close()

	// 4. 解析键值对 (key=value)
	config := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		config[key] = value
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("读取配置文件时出错，将尽可能使用已解析的值", "error", err)
	}

	// 5. 使用配置文件中的值覆盖默认值
	if val, ok := config["OJ_HOST_NAME"]; ok {
		dbHost = val
	}
	if val, ok := config["OJ_PORT_NUMBER"]; ok {
		if port, err := strconv.Atoi(val); err == nil {
			dbPort = port
		} else {
			slog.Warn("无效的 OJ_PORT_NUMBER", "value", val, "default", dbPort)
		}
	}
	if val, ok := config["OJ_USER_NAME"]; ok {
		dbUser = val
	}
	if val, ok := config["OJ_PASSWORD"]; ok {
		dbPass = val
	}
	if val, ok := config["OJ_DB_NAME"]; ok {
		dbName = val
	}

	// 6. 记录最终配置 (注意：不要记录密码)
	slog.Info("配置加载成功")
	slog.Info("  OJ_HOME", "value", ojHome)
	slog.Info("  DB_HOST", "value", dbHost)
	slog.Info("  DB_PORT", "value", dbPort)
	slog.Info("  DB_NAME", "value", dbName)
	slog.Info("  DB_USER", "value", dbUser)
}

// --- 数据库交互 ---

var db *sql.DB

// initMySQLConn (使用 slog)
func initMySQLConn() error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8",
		dbUser, dbPass, dbHost, dbPort, dbName)

	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("无法打开数据库连接: %v", err)
	}

	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	if err = db.Ping(); err != nil {
		return fmt.Errorf("无法连接到数据库: %v", err)
	}

	if _, err = db.Exec("SET NAMES utf8"); err != nil {
		return fmt.Errorf("无法设置 UTF8: %v", err)
	}

	slog.Info("数据库连接成功")
	return nil
}

// getSolutionInfo 对应 C++ 的 _get_solution_info_mysql
func getSolutionInfo(solutionID int) (pID int, userID string, lang int, cID int, err error) {
	query := fmt.Sprintf("SELECT problem_id, user_id, language, contest_id FROM %s WHERE solution_id = ?", tbName)
	var nullCID sql.NullInt64
	err = db.QueryRow(query, solutionID).Scan(&pID, &userID, &lang, &nullCID)
	if err != nil {
		return 0, "", 0, 0, fmt.Errorf("获取提交信息失败: %v", err)
	}
	if nullCID.Valid {
		cID = int(nullCID.Int64)
	} else {
		cID = 0
	}
	return pID, userID, lang, cID, nil
}

// getProblemInfo 对应 C++ 的 _get_problem_info_mysql
func getProblemInfo(pID int) (timeLimit float64, memLimit int, spj int, err error) {
	query := "SELECT time_limit, memory_limit, spj FROM problem WHERE problem_id = ?"
	err = db.QueryRow(query, pID).Scan(&timeLimit, &memLimit, &spj)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("获取题目信息失败: %v", err)
	}
	return timeLimit, memLimit, spj, nil
}

// getSolution 对应 C++ 的 _get_solution_mysql
func getSolution(solutionID int) (source string, err error) {
	query := "SELECT source FROM source_code WHERE solution_id = ?"
	err = db.QueryRow(query, solutionID).Scan(&source)
	if err != nil {
		return "", fmt.Errorf("获取源代码失败: %v", err)
	}
	return source, nil
}

// updateSolution (使用 slog)
func updateSolution(solutionID int, result int, time int, memory int, passRate float64) error {
	query := fmt.Sprintf(
		"UPDATE %s SET result=?, time=?, memory=?, pass_rate=?, judger=?, judgetime=now() WHERE solution_id=?",
		tbName,
	)
	_, err := db.Exec(query, result, time, memory, passRate, httpJudgerName, solutionID)
	if err != nil {
		return fmt.Errorf("更新提交状态失败: %v", err)
	}
	slog.Info("更新 Solution", "result", result, "time_ms", time, "memory_kb", memory, "pass_rate", passRate)
	return nil
}

// updateUser (使用 slog)
func updateUser(userID string) error {
	querySolved := "UPDATE `users` SET `solved`=(SELECT count(DISTINCT `problem_id`) FROM `solution` s WHERE s.`user_id`=? AND s.`result`=4 AND problem_id>0 AND problem_id NOT IN (SELECT problem_id FROM contest_problem WHERE contest_id IN (SELECT contest_id FROM contest WHERE contest_type & 16 > 0 AND end_time>now()))) WHERE `user_id`=?"
	if _, err := db.Exec(querySolved, userID, userID); err != nil {
		slog.Warn("更新用户 Solved 失败", "user_id", userID, "error", err)
	}

	querySubmit := "UPDATE `users` SET `submit`=(SELECT count(DISTINCT `problem_id`) FROM `solution` s WHERE s.`user_id`=? AND problem_id>0 AND problem_id NOT IN (SELECT problem_id FROM contest_problem WHERE contest_id IN (SELECT contest_id FROM contest WHERE contest_type & 16 > 0 AND end_time>now()))) WHERE `user_id`=?"
	if _, err := db.Exec(querySubmit, userID, userID); err != nil {
		slog.Warn("更新用户 Submit 失败", "user_id", userID, "error", err)
	}

	slog.Info("更新用户统计", "user_id", userID)
	return nil
}

// updateProblem (使用 slog)
func updateProblem(pID int, cID int) error {
	if cID > 0 {
		queryContestAccepted := "UPDATE `contest_problem` SET `c_accepted`=(SELECT count(*) FROM `solution` WHERE `problem_id`=? AND `result`=4 AND contest_id=?) WHERE `problem_id`=? AND contest_id=?"
		if _, err := db.Exec(queryContestAccepted, pID, cID, pID, cID); err != nil {
			slog.Warn("更新竞赛题目 Accepted 失败", "problem_id", pID, "contest_id", cID, "error", err)
		}
		queryContestSubmit := "UPDATE `contest_problem` SET `c_submit`=(SELECT count(*) FROM `solution` WHERE `problem_id`=? AND contest_id=?) WHERE `problem_id`=? AND contest_id=?"
		if _, err := db.Exec(queryContestSubmit, pID, cID, pID, cID); err != nil {
			slog.Warn("更新竞赛题目 Submit 失败", "problem_id", pID, "contest_id", cID, "error", err)
		}
	}

	queryProblemAccepted := "UPDATE `problem` SET `accepted`=(SELECT count(*) FROM `solution` s WHERE s.`problem_id`=? AND s.`result`=4 AND problem_id NOT IN (SELECT problem_id FROM contest_problem WHERE contest_id IN (SELECT contest_id FROM contest WHERE contest_type & 16 > 0 AND end_time>now()))) WHERE `problem_id`=?"
	if _, err := db.Exec(queryProblemAccepted, pID, pID); err != nil {
		slog.Warn("更新主题目 Accepted 失败", "problem_id", pID, "error", err)
	}

	slog.Info("更新题目统计", "problem_id", pID)
	return nil
}

// --- 核心功能 (部分为 Stub) ---

// writeSourceCode (使用 slog)
func writeSourceCode(source string, lang int, workDir string) error {
	ext1, ok := langMaps[lang]
	if !ok {
		return fmt.Errorf("未知的语言 ID: %d", lang)
	}
	ext := ext1.Suffix
	fileName := fmt.Sprintf("Main%s", ext)
	filePath := filepath.Join(workDir, fileName)
	err := os.WriteFile(filePath, []byte(source), 0644)
	if err != nil {
		return fmt.Errorf("写入源代码失败: %v", err)
	}
	slog.Info("源代码已写入", "path", filePath)
	return nil
}

// compile (Stub, 使用 slog)
func compile(lang int, rootDir string) *models.SandboxOutput {
	// judge-sandbox -rootfs=xxx -cmd=yyy -cwd=/code
	fmt.Println("cmd=", langDetail.Cmd.Compile)
	selfname, _ := os.Executable()
	cmd := exec.Command(selfname,
		"sandbox",
		fmt.Sprintf("--rootfs=%s", rootDir),
		fmt.Sprintf("--cmd=%s", langDetail.Cmd.Compile),
		fmt.Sprintf("--time=%d", 3000),
		fmt.Sprintf("--memory=%d", 256<<10),
		fmt.Sprintf("--sid=%d", rsolutionID),
		"--cwd=/code",
	)
	if len(langDetail.Cmd.Env) > 0 {
		cmd.Env = append(cmd.Env, langDetail.Cmd.Env...)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout

	r3, w3, err := os.Pipe()
	if err != nil {
		return &models.SandboxOutput{UserStatus: constants.OJ_SE, CombinedOutput: "failed to create pipe for compile"}
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, w3)
	slog.Info("STUB: 正在编译...", "language", lang, "work_dir", rootDir)
	err = cmd.Start()
	if err != nil {
		return &models.SandboxOutput{UserStatus: constants.OJ_SE, CombinedOutput: "failed to start compile command"}
	}
	w3.Close()
	var output models.SandboxOutput
	json.NewDecoder(r3).Decode(&output)
	slog.Info("debug", "output", output)
	cmd.Wait()
	return &output
}

// addCEInfo (Stub, 使用 slog)
func addCEInfo(solutionID int, msg string) error {
	slog.Info("STUB: 正在添加编译错误信息", "msg", msg)
	_, err := db.Exec("DELETE FROM compileinfo WHERE solution_id=?", solutionID)
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}
	_, err = db.Exec("INSERT INTO compileinfo VALUES(?, ?)", solutionID, msg)
	if err != nil {
		return fmt.Errorf("insert failed: %w", err)
	}
	return nil
}

func findDataFiles(pID int) ([][]string, error) {
	dataDir := filepath.Join(ojHome, "data", strconv.Itoa(pID))
	slog.Info("正在扫描数据文件", "directory", dataDir)

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		// 如果目录不存在，这不是一个致命错误，只是意味着没有测试数据。
		if os.IsNotExist(err) {
			slog.Warn("数据目录不存在，未找到测试用例", "directory", dataDir)
			return [][]string{}, nil // 返回空切片，而不是错误
		}
		// 其他错误（例如权限问题）是致命的
		slog.Error("读取数据目录失败", "directory", dataDir, "error", err)
		return nil, fmt.Errorf("读取数据目录失败 %s: %v", dataDir, err)
	}

	var inFiles []string
	// 1. 查找所有 .in 文件
	for _, entry := range entries {
		// 忽略子目录
		if entry.IsDir() {
			continue
		}

		fileName := entry.Name()
		if filepath.Ext(fileName) == ".in" {
			inFiles = append(inFiles, fileName)
		}
	}

	// 2. 对 .in 文件进行排序，以确保判题顺序
	sort.Strings(inFiles)
	slog.Info("已找到 .in 文件", "count", len(inFiles))

	// 3. 构建配对
	var result [][]string
	for _, inFileName := range inFiles {
		inFullPath := filepath.Join(dataDir, inFileName)

		// 4. 构造对应的 .out 文件路径
		baseName := strings.TrimSuffix(inFileName, ".in")
		outFileName := baseName + ".out"
		outFullPath := filepath.Join(dataDir, outFileName)

		outPath := "" // 默认 .out 路径为空字符串

		// 5. 检查 .out 文件是否真实存在
		if _, err := os.Stat(outFullPath); err == nil {
			// 文件存在
			outPath = outFullPath
		} else if !os.IsNotExist(err) {
			// 如果错误不是 "不存在" (例如：权限问题)，则记录一个警告
			slog.Warn("无法访问 .out 文件 (将视为空)", "path", outFullPath, "error", err)
		}
		// 如果文件 os.IsNotExist(err)，outPath 保持为 ""

		// 6. 添加配对
		result = append(result, []string{inFullPath, outPath})
	}

	slog.Info("数据文件配对完成", "pairs", len(result))
	return result, nil
}
func findInName(pID int) string {
	inNameFile := filepath.Join(ojHome, "data", strconv.Itoa(pID), "input.name")
	bt, err := os.ReadFile(inNameFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(bt))
}
func findOutName(pID int) string {
	outNameFile := filepath.Join(ojHome, "data", strconv.Itoa(pID), "output.name")
	bt, err := os.ReadFile(outNameFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(bt))
}

// CopyFile copies the file from src to dst.
func CopyFile(src, dst string) error {
	// 1. 打开源文件
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	// 2. 创建目标文件
	// 确保目标目录存在，如果不存在则创建
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}
	destinationFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destinationFile.Close()

	// 3. 使用 io.Copy 进行文件内容复制
	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	// 4. 可选：复制文件权限
	sourceInfo, err := os.Stat(src)
	if err == nil { // 如果无法获取源文件信息，则忽略权限复制
		if err := os.Chmod(dst, sourceInfo.Mode()); err != nil {
			return fmt.Errorf("failed to set file permissions: %w", err)
		}
	}

	return nil
}

type RunConfig struct {
	Lang        int
	Rootdir     string
	Workdir     string
	InFile      string
	OutFile     string
	InName      string
	OutName     string
	Timelimit   int
	MemoryLimit int
	Spj         int
}

// runAndCompare (Stub, 使用 slog)
func runAndCompare(rcfg RunConfig) (result int, timeUsed int, memUsed int) {
	// var lang int, rootDir string, workDir string, inFile string, outFile string, timeLimit int, memoryLimit int, spj bool;
	slog.Info("STUB: 正在运行和比对", "in_file", rcfg.InFile, "out_file", rcfg.OutFile)
	// handle inName
	stdinName := "/code/data.in"
	stdoutName := "/code/data.usr"
	if rcfg.InName != "" {
		CopyFile(rcfg.InFile, filepath.Join(rcfg.Workdir, rcfg.InName))
		stdinName = ""
	} else {
		CopyFile(rcfg.InFile, filepath.Join(rcfg.Workdir, "data.in"))
	}
	if rcfg.OutName != "" {
		stdoutName = ""
	}
	var runArgs []string = []string{
		"sandbox",
		fmt.Sprintf("--rootfs=%s", rcfg.Rootdir),
		fmt.Sprintf("--cmd=%s", langDetail.Cmd.Run),
		fmt.Sprintf("--time=%d", rcfg.Timelimit),         // in milisecond
		fmt.Sprintf("--memory=%d", rcfg.MemoryLimit<<10), // in kb
		fmt.Sprintf("--sid=%d", rsolutionID),
		"--cwd=/code",
	}
	if stdinName != "" {
		runArgs = append(runArgs, fmt.Sprintf("--stdin=%s", stdinName))
	}
	if stdoutName != "" {
		runArgs = append(runArgs, fmt.Sprintf("--stdout=%s", stdoutName))
	}

	selfname, _ := os.Executable()
	cmd := exec.Command(selfname, runArgs...)
	if len(langDetail.Cmd.Env) > 0 {
		cmd.Env = append(cmd.Env, langDetail.Cmd.Env...)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	r3, w3, err := os.Pipe()
	if err != nil {
		return constants.OJ_SE, 0, 0
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, w3)
	slog.Info("STUB: 正在运行...", "language", rcfg.Lang, "work_dir", rcfg.Rootdir, "data", rcfg.InFile)
	err = cmd.Start()
	fmt.Println(err)
	w3.Close()
	cmd.Wait()

	var output models.SandboxOutput
	err = json.NewDecoder(r3).Decode(&output)
	slog.Info("debug", "output", output, "err", err)

	result = output.UserStatus
	timeUsed = output.Time
	memUsed = output.Memory
	if result != constants.OJ_AC {
		return
	}

	// do spj here

	// compare the results
	targetOutputName := "data.usr"
	if rcfg.OutName != "" {
		targetOutputName = rcfg.OutName
	}
	targetInputName := "data.in"
	if rcfg.InName != "" {
		targetInputName = rcfg.InName
	}
	if rcfg.Spj == 0 {
		res, err := compareFiles(rcfg.OutFile, filepath.Join(rcfg.Rootdir, "code", targetOutputName))
		switch res {
		case 1:
			result = constants.OJ_PE
		case 2:
			result = constants.OJ_WA
		case 0:
			result = constants.OJ_AC
		}
		if err != nil {
			result = constants.OJ_RE
		}
		return
	}
	if rcfg.Spj == 1 {
		// data
		var sysdatafile = filepath.Join(rcfg.Rootdir, "/code/sysdata.out")
		CopyFile(rcfg.OutFile, sysdatafile)
		defer os.Remove(sysdatafile)
		// spj
		var spjfile = filepath.Join(rcfg.Rootdir, "code/spj")
		CopyFile(filepath.Join(filepath.Dir(rcfg.OutFile), "spj"), spjfile)
		defer os.Remove(spjfile)
		// run cmd
		var runArgs []string = []string{
			"sandbox",
			fmt.Sprintf("--rootfs=%s", filepath.Join(rcfg.Rootdir, "code")),
			fmt.Sprintf("--cmd=/spj %s %s %s", targetInputName, targetOutputName, "sysdata.out"),
			fmt.Sprintf("--time=%d", rcfg.Timelimit),         // in milisecond
			fmt.Sprintf("--memory=%d", rcfg.MemoryLimit<<10), // in kb
			fmt.Sprintf("--sid=%d", rsolutionID),
			"--cwd=/",
		}

		pipeRead, pipeWrite, err := os.Pipe()
		if err != nil {
			panic(fmt.Errorf("os.Pipe() failed: %s", err))
		}
		defer pipeRead.Close()

		slog.Info("runArgs", "args", runArgs)
		cmd := exec.Command(selfname, runArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.ExtraFiles = []*os.File{pipeWrite}
		cmd.Start()
		pipeWrite.Close()
		err = cmd.Wait()
		slog.Info("run err", "err", err)
		var output2 models.SandboxOutput
		json.NewDecoder(pipeRead).Decode(&output2)

		slog.Info("exitoutput", "outputdata", output2)
		if output2.ExitStatus == 0 {
			result = constants.OJ_AC
		} else {
			result = constants.OJ_WA
		}
	}
	return
}

// addREInfo (Stub, 使用 slog)
func addREInfo(solutionID int) {
	_ = solutionID
	slog.Info("STUB: 添加运行错误信息")
}

// addDiffInfo (Stub, 使用 slog)
func addDiffInfo(solutionID int) {
	_ = solutionID
	slog.Info("STUB: 添加 Diff 详情")
}

// cleanWorkDir (Stub, 使用 slog)
func cleanWorkDir(workDir string) {
	slog.Info("STUB: 正在清理工作目录", "path", workDir)
	if err := os.RemoveAll(workDir); err != nil {
		slog.Warn("清理工作目录失败", "path", workDir, "error", err)
	}
}

// --- Main 工作流 ---

func Main() {
	// 0. 设置 slog
	// 使用 JSON Handler 以便进行结构化日志记录
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	var nArgs = os.Args[1:]

	// 1. 初始化参数
	if len(nArgs) < 3 {
		fmt.Println("用法: <> client <solution_id> <runner_id> [oj_home_path]")
		os.Exit(1)
	}

	debug := false
	if len(nArgs) > 4 && nArgs[4] == "DEBUG" {
		debug = true
	}

	solutionID, err := strconv.Atoi(nArgs[1])
	rsolutionID = solutionID
	if err != nil {
		slog.Error("无效的 Solution ID", "input", nArgs[1])
		os.Exit(1)
	}

	// 使用 slog.With 创建一个包含 solution_id 的新 logger，并设为默认
	slog.SetDefault(slog.Default().With("solution_id", solutionID))

	runnerID := nArgs[2]
	homePath := "/home/judge"
	if len(nArgs) > 3 {
		homePath = nArgs[3]
	}

	slog.Info("开始判题", "runner_id", runnerID)

	// 2. 初始化配置和数据库
	initJudgeConf(homePath)
	if err := initMySQLConn(); err != nil {
		slog.Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// 3. 读取语言支持列表
	langMaps = getLangMaps(filepath.Join(homePath, "etc", "langs", "all.toml"))

	// 4. 获取判题信息
	pID, userID, lang, cID, err := getSolutionInfo(solutionID)
	if err != nil {
		slog.Error("获取提交信息失败", "error", err)
		os.Exit(1)
	}
	slog.Info("获取信息", "problem_id", pID, "user_id", userID, "language", lang, "contest_id", cID)

	timeLimit, memLimit, spj, err := getProblemInfo(pID)
	if err != nil {
		slog.Error("获取题目信息失败", "error", err)
		os.Exit(1)
	}
	slog.Info("题目限制", "time_limit_s", timeLimit, "mem_limit_mb", memLimit, "spj", spj)

	// 获取语言对应的环境配置信息
	var errLang error
	langDetail, errLang = getLangDetails(lang)
	if errLang != nil {
		slog.Error("获取语言详情失败", "err", errLang)
		os.Exit(1)
	}

	// TODO: 准备工作目录 使用 overlay2, base作为lower,自建一个upper,merged,workdir,最后操作merged
	workBaseDir := filepath.Join(ojHome, "run"+runnerID)
	for _, zdir := range []string{"rootfs", "tmp"} {
		toCreateDir := filepath.Join(workBaseDir, zdir)
		if err := os.MkdirAll(toCreateDir, 0755); err != nil {
			slog.Error("创建工作目录失败", "path", toCreateDir, "error", err)
			os.Exit(1)
		}
	}
	if !debug {
		defer cleanWorkDir(workBaseDir)
	}

	tmpfsDir := filepath.Join(workBaseDir, "tmp")
	tmpfsSize := "size=580M"
	// tmpfs to <workbase>/tmp/
	err = unix.Mount("tmpfs", tmpfsDir, "tmpfs", uintptr(unix.MS_NOSUID|unix.MS_NODEV), tmpfsSize)
	if err != nil {
		slog.Error("挂载 tmpfs 失败", "err", err)
		os.Exit(1)
	}
	if !debug {
		defer unix.Unmount(tmpfsDir, 0)
	}
	// do mount here

	for _, zdir := range []string{"upper", "work"} {
		os.MkdirAll(filepath.Join(tmpfsDir, zdir), 0755)
	}

	options := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
		langDetail.Fs.Base,
		filepath.Join(workBaseDir, "tmp", "upper"),
		filepath.Join(workBaseDir, "tmp", "work"),
	)

	// 3. 设置挂载参数
	fstype := "overlay"
	fsource := "overlay" // 对于 overlayfs，source 通常就是 "overlay" 或 "none"
	flags := uintptr(0)  // 默认挂载标志
	rootfs := filepath.Join(workBaseDir, "rootfs")
	fmt.Printf("mount -t overlay overlay -o %s %s\n", options, rootfs)
	if err := unix.Mount(fsource, rootfs, fstype, flags, options); err != nil {
		slog.Error("挂载 overlayfs 失败", "err", err)
		os.Exit(1)
	}

	if !debug {
		defer unix.Unmount(rootfs, 0)
	}
	// 5. 获取并写入源代码
	source, err := getSolution(solutionID)
	if err != nil {
		slog.Error("获取源代码失败", "error", err)
		os.Exit(1)
	}
	workdir := filepath.Join(rootfs, "code")
	err = os.MkdirAll(workdir, 0777)
	if err != nil {
		slog.Error("创建代码工作目录失败", "path", workdir, "err", err)
		os.Exit(1)
	}
	os.Chmod(workdir, 0777)
	if err := writeSourceCode(source, lang, filepath.Join(rootfs, "code")); err != nil {
		slog.Error("写入源代码失败", "error", err)
		os.Exit(1)
	}

	// 6. 编译 (Stub)
	if err := updateSolution(solutionID, constants.OJ_CI, 0, 0, 0.0); err != nil { // 设置为编译中
		slog.Warn("更新到 '编译中' 失败", "error", err)
	}

	compileResult := compile(lang, rootfs)
	if compileResult.ExitStatus != 0 {
		slog.Info("编译失败", "output", compileResult.CombinedOutput)
		addCEInfo(solutionID, compileResult.CombinedOutput)
		if err := updateSolution(solutionID, constants.OJ_CE, 0, 0, 0.0); err != nil {
			slog.Error("更新 '编译失败' 状态失败", "error", err)
			os.Exit(1)
		}
		updateUser(userID)
		updateProblem(pID, cID)
		return
	}

	if err := updateSolution(solutionID, constants.OJ_RI, 0, 0, 0.0); err != nil { // 设置为运行中
		slog.Warn("更新到 '运行中' 失败", "error", err)
	}

	// 7. 运行和比对 (Stub)
	dataFiles, err := findDataFiles(pID)
	if err != nil {
		slog.Error("查找数据文件失败", "error", err)
		return
	}
	// TODO: input.name output.name
	inName := findInName(pID)
	outName := findOutName(pID)
	var (
		totalTime  = 0
		peakMemory = 0
		passRate   = 0.0
		testCases  = float64(len(dataFiles))
	)

	var rCfg RunConfig = RunConfig{Lang: lang,
		Rootdir: rootfs, Workdir: workdir,
		Timelimit: int(1000 * timeLimit), MemoryLimit: memLimit,
		InName: inName, OutName: outName,
		Spj: spj}

	var tot models.TotalResults
	tot.FinalResult = constants.OJ_AC

	for _, dataFile := range dataFiles {
		rCfg.InFile = dataFile[0]
		rCfg.OutFile = dataFile[1]

		result, timeUsed, memUsed := runAndCompare(rCfg)

		if timeUsed > totalTime {
			totalTime = timeUsed
		}
		if memUsed > peakMemory {
			peakMemory = memUsed
		}

		filename := filepath.Base(dataFile[0])
		if result != constants.OJ_AC {
			if tot.FinalResult == constants.OJ_AC {
				tot.FinalResult = result
			}
			tot.Results = append(tot.Results, models.OneResult{Result: result, Datafile: filename, Time: timeUsed, Mem: memUsed}) //nolint:all
			slog.Warn("测试点失败", "data_file", filename, "result", result)
			// break
		} else {
			tot.Results = append(tot.Results, models.OneResult{Result: result, Datafile: filename, Time: timeUsed, Mem: memUsed})
			passRate += 1.0
			slog.Info("测试点通过", "data_file", filename)
		}
	}

	// 8. 处理最终结果
	if testCases > 0 {
		passRate = passRate / testCases
	} else if tot.FinalResult == constants.OJ_AC {
		passRate = 1.0
	}

	switch tot.FinalResult {
	case constants.OJ_RE:
		addREInfo(solutionID)
	case constants.OJ_WA, constants.OJ_PE:
		addDiffInfo(solutionID)
	}

	// 9. 更新数据库
	slog.Info("判题完成", "final_result", tot.FinalResult, "total_time_ms", totalTime, "peak_mem_kb", peakMemory, "pass_rate", passRate) //nolint:all
	slog.Info("判题结果", "FF", tot)
	if err := updateSolution(solutionID, tot.FinalResult, totalTime, peakMemory, passRate); err != nil {
		slog.Error("更新最终判题结果失败", "error", err)
		os.Exit(1)
	}

	if err := updateUser(userID); err != nil {
		slog.Warn("更新用户统计失败", "error", err)
	}

	if err := updateProblem(pID, cID); err != nil {
		slog.Warn("更新题目统计失败", "error", err)
	}

	slog.Info("判题流程结束")
}
