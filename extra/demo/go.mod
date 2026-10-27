// 示例题库是数据资产，不是 Go 模块：参考解目录里同时放了 .c 与 .go，
// 会让主模块的 `go build ./...` 报 “C source files not allowed when not
// using cgo or SWIG”。这里放一个独立的 go.mod，让 Go 工具链整棵跳过
// extra/demo（该文件不会打进部署包，见 extra/package.sh）。
module github.com/sempr/hustoj-go/extra/demo

go 1.24
