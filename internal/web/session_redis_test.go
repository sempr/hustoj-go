// 测试专用的 Go 会话存储装配。
//
// 单测不连生产的 Redis：那会让测试依赖外部服务状态（残留 key、
// 网络抖动、主从切换都会变成难以复现的偶发失败），CI 上更是根本没有 Redis。
//
// 用 miniredis 起一个进程内的真 Redis：go-redis 的命令序列
// （包括 WATCH/MULTI/EXEC 与 TTL）都按真实语义执行，所以这里能覆盖到
// 真正的乐观锁重试路径，而不是拿一个只会 Get/Set 的假 map 把逻辑测空。

package web

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// newTestSessionStore 起一个隔离的 Redis 并返回对应的会话存储。
//
// 每个用例一份，互不干扰；用例结束自动关闭。
func newTestSessionStore(t *testing.T) *GoSessionStore {
	t.Helper()

	mr := miniredis.RunT(t)
	store, err := NewGoSessionStore("redis://"+mr.Addr()+"/0", time.Hour)
	if err != nil {
		t.Fatalf("NewGoSessionStore: %v", err)
	}
	t.Cleanup(func() {
		// 先确认连接可用再关，否则 miniredis 已退出时 Close 的错误
		// 会在清理阶段报出来，掩盖真正的测试失败。
		_ = store.rdb.Ping(store.rdb.Context()).Err()
		_ = store.Close()
	})
	return store
}
