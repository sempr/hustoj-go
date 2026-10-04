// Go 自身的会话存储：登录态与 CSRF 全部存在 Redis 里。
//
// 为什么不再写 PHP 会话文件：
//
//  1. 两端同时写同一批文件，谁最后落盘谁生效，登录态会在切流时随机翻转；
//  2. PHP 会话的生命周期、垃圾回收、序列化格式都归 PHP 所有，
//     Go 只能"配合"而不能"主宰"，一旦 PHP 下线就要整体重写；
//  3. 写 user_id 进 PHP 会话，等于把"谁拥有登录态"这个问题在两端各答一次。
//
// 现在的分工是干净的：
//   - PHP 会话目录：只读。仅用于识别切流前就已登录的老用户。
//   - Redis：Go 的登录态与 CSRF 的唯一真相来源。
//
// Redis 选它而不是 JWT，是因为登录态必须可即时吊销：封号、改密码、
// 考试模式 IP 绑定都需要"立刻失效"，而无状态 token 只能等 TTL 到期。
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// GoSession 是存在 Redis 里的一条会话。
//
// 用 JSON 而非 PHP 序列化：Go 会话不需要与 PHP 互读，
// JSON 便于排障（redis-cli GET 直接可读），且字段增删不破坏旧数据。
type GoSession struct {
	// UserID 为空表示匿名会话（只用来挂 CSRF token 与验证码）。
	UserID     string          `json:"uid,omitempty"`
	Nick       string          `json:"nick,omitempty"`
	GroupName  string          `json:"group,omitempty"`
	Privileges GoPrivileges    `json:"priv,omitempty"`
	ContestIDs map[string]bool `json:"contest,omitempty"`
	// VCode 是当前验证码的正确答案，VFail 表示上一次校验失败。
	//
	// 两者对应 PHP 的 <oj>_vcode / <oj>_vfail。VFail 不只是个标记：
	// vcode.php 会据此把验证码从 4 位数字换成 8 位数字（见 phpweb/vcode.php:38-42），
	// 即"失败一次就加大难度"，所以它必须跟着会话持久化。
	VCode string `json:"vcode,omitempty"`
	VFail bool   `json:"vfail,omitempty"`
	// CreatedAt 用于会话固定攻击检测：登录成功后换新会话时，
	// 若请求带来的会话比这个时间还早，说明是攻击者预置的。
	CreatedAt int64 `json:"iat"`
}

// GoPrivileges 是会话里携带的权限位。
//
// 权限来自登录那一刻的数据库快照，而不是每个请求都查库：
// 查库能让封号立刻生效，但每个请求多一次查询，
// 而 PHP 侧本来就是"登录时写入会话"的行为，这里保持一致。
// 需要立即生效的变更（封号）走 loginlog 与 user 表的在线判定。
type GoPrivileges struct {
	Admin           bool `json:"admin,omitempty"`
	ContestCreator  bool `json:"cc,omitempty"`
	ProblemEditor   bool `json:"pe,omitempty"`
	ProblemVerifier bool `json:"pv,omitempty"`
	SourceBrowser   bool `json:"sb,omitempty"`
}

// errSessionNotFound 表示会话不存在或已过期。
var errSessionNotFound = errors.New("会话不存在或已过期")

// GoSessionStore 是 Redis 上的 Go 会话存储。
type GoSessionStore struct {
	rdb *redis.Client
	ttl time.Duration
	// prefix 隔离键空间，避免与 judge 服务共用 Redis DB 时互相踩踏。
	prefix string
}

// NewGoSessionStore 连接 Redis 并返回会话存储。
//
// 这里不 ping：配置阶段的网络抖动不应该让服务起不来，
// 真正访问会话时的错误会由调用方按"匿名"降级处理。
func NewGoSessionStore(redisURL string, ttl time.Duration) (*GoSessionStore, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("解析 redis_url 失败: %w", err)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("会话 TTL 必须为正数，收到 %s", ttl)
	}
	return &GoSessionStore{
		rdb:    redis.NewClient(opt),
		ttl:    ttl,
		prefix: "hustoj:gosess:",
	}, nil
}

// Close 关闭底层连接。
func (g *GoSessionStore) Close() error { return g.rdb.Close() }

func (g *GoSessionStore) key(sid string) string { return g.prefix + sid }

// Get 读取会话；不存在返回 errSessionNotFound。
func (g *GoSessionStore) Get(ctx context.Context, sid string) (*GoSession, error) {
	if !sessionIDAllowed(sid) {
		return nil, errSessionNotFound
	}
	raw, err := g.rdb.Get(ctx, g.key(sid)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, errSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("读取会话失败: %w", err)
	}
	var s GoSession
	if err := json.Unmarshal(raw, &s); err != nil {
		// 会话数据损坏（Redis 被手工改过、或跨版本反序列化失败）：
		// 当作不存在，让用户重新登录，而不是把整个站点的请求打成 500。
		return nil, errSessionNotFound
	}
	return &s, nil
}

// Save 写入会话并续期 TTL。
func (g *GoSessionStore) Save(ctx context.Context, sid string, s *GoSession) error {
	if !sessionIDAllowed(sid) {
		return fmt.Errorf("非法的会话 ID")
	}
	if s.CreatedAt == 0 {
		s.CreatedAt = time.Now().Unix()
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("序列化会话失败: %w", err)
	}
	if err := g.rdb.Set(ctx, g.key(sid), raw, g.ttl).Err(); err != nil {
		return fmt.Errorf("写入会话失败: %w", err)
	}
	return nil
}

// Destroy 删除会话，对应 PHP 的 session_destroy()。
func (g *GoSessionStore) Destroy(ctx context.Context, sid string) error {
	if !sessionIDAllowed(sid) {
		return nil
	}
	if err := g.rdb.Del(ctx, g.key(sid)).Err(); err != nil {
		return fmt.Errorf("删除会话失败: %w", err)
	}
	return nil
}

// Touch 续期但不改动内容，对应 PHP 的 gc_maxlifetime 滚动窗口。
func (g *GoSessionStore) Touch(ctx context.Context, sid string) error {
	if !sessionIDAllowed(sid) {
		return errSessionNotFound
	}
	return g.rdb.Expire(ctx, g.key(sid), g.ttl).Err()
}

// Update 在乐观锁内读-改-写会话，返回更新后的副本。
//
// 用 WATCH/MULTI 而不是 Lua 脚本：单次 CSRF 消费的临界区只有几十微秒，
// 冲突概率极低，重试即可；Lua 要自己实现重试上限与退避，代码量反而更大。
//
// 临界区不可分割的原因：CSRF token 单次消费必须"读到 → 判定 → 写回"
// 一次完成，否则并发的两次 POST 各自读到同一个 token，会双双通过校验 ——
// 这正是 token 池被设计成单次消费要防的事。
func (g *GoSessionStore) Update(ctx context.Context, sid string, fn func(*GoSession) error) (*GoSession, error) {
	if !sessionIDAllowed(sid) {
		return nil, errSessionNotFound
	}
	const maxRetries = 5
	key := g.key(sid)

	for attempt := 0; attempt < maxRetries; attempt++ {
		var result GoSession
		err := g.rdb.Watch(ctx, func(tx *redis.Tx) error {
			cur, err := tx.Get(ctx, key).Bytes()
			if errors.Is(err, redis.Nil) {
				return errSessionNotFound
			}
			if err != nil {
				return fmt.Errorf("读取会话失败: %w", err)
			}
			var s GoSession
			if err := json.Unmarshal(cur, &s); err != nil {
				return errSessionNotFound
			}

			if err := fn(&s); err != nil {
				return err
			}
			if s.CreatedAt == 0 {
				s.CreatedAt = time.Now().Unix()
			}
			raw, err := json.Marshal(&s)
			if err != nil {
				return fmt.Errorf("序列化会话失败: %w", err)
			}
			// 提交点：若 key 自 WATCH 以来被改过，这里返回 TxFailedErr。
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, raw, g.ttl)
				return nil
			})
			if err != nil {
				return err
			}
			result = s
			return nil
		}, key)

		if err == nil {
			return &result, nil
		}
		if !isTxConflict(err) {
			return nil, err
		}
		// 冲突：重新读取再试。不 sleep —— 冲突窗口只有几十微秒，
		// 退避反而会让高并发下的重试全部撞在一起。
	}
	return nil, fmt.Errorf("会话更新冲突次数超过 %d 次", maxRetries)
}

// isTxConflict 判断错误是否为 WATCH 冲突（需要重试）。
func isTxConflict(err error) bool {
	return errors.Is(err, redis.TxFailedErr)
}
