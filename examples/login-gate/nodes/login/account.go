package login

import (
	"fmt"
	"sync"

	ccrypto "github.com/gulurence/cherry-server/extend/crypto"
)

// accountRecord 是一条内存账号记录。示例为了不依赖数据库，账号放在内存里，
// 进程重启即清空；真实项目应把这里的存储替换成数据库访问。
type accountRecord struct {
	uid      int64
	password string // 只存 md5，不存明文
}

// accountStore 是一个并发安全的内存账号存储。
type accountStore struct {
	mu       sync.RWMutex
	accounts map[string]*accountRecord // account -> record
	nextUID  int64
}

// newAccountStore 创建账号存储，并预置两个演示账号方便直接测试。
func newAccountStore() *accountStore {
	s := &accountStore{
		accounts: make(map[string]*accountRecord),
		nextUID:  10000,
	}

	s.Register("admin", "admin123")
	s.Register("test", "123456")

	// 压测用：预置 1000 个账号 stress0..stress999，避免同一账号重复登录互相顶号。
	for i := 0; i < 1000; i++ {
		s.Register(fmt.Sprintf("stress%d", i), "123456")
	}

	return s
}

// Register 注册账号，返回新分配的 uid。账号已存在返回 (0, false)。
func (s *accountStore) Register(account, password string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.accounts[account]; ok {
		return 0, false
	}

	s.nextUID++
	s.accounts[account] = &accountRecord{uid: s.nextUID, password: ccrypto.MD5(password)}
	return s.nextUID, true
}

// Verify 校验账号密码，返回 uid。账号不存在或密码错误返回 0。
func (s *accountStore) Verify(account, password string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.accounts[account]
	if !ok || rec.password != ccrypto.MD5(password) {
		return 0
	}
	return rec.uid
}
