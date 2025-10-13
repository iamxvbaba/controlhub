package controlhub

import (
	"context"
	"net/http"
)

// Authenticator 定义认证接口，返回 context、clientID 和 error
type Authenticator interface {
	Authenticate(r *http.Request) (context.Context, string, error)
}

// SecretIDAuth 基于 id + secret 的简单认证
// 从 Header 读取 (X-Client-ID, X-Client-Secret)，若不存在则回退到查询参数 (?id=&secret=)
type SecretIDAuth struct {
	Secret       string
	IDHeader     string // 默认 X-Client-ID
	SecretHeader string // 默认 X-Client-Secret
}

// Authenticate: 仅校验 secret 是否与预期一致，返回请求携带的 id 作为 clientID
func (a *SecretIDAuth) Authenticate(r *http.Request) (context.Context, string, error) {
	if a == nil || a.Secret == "" {
		return nil, "", ErrUnauthorized
	}
	idHeader := a.IDHeader
	if idHeader == "" {
		idHeader = "X-Client-ID"
	}
	secretHeader := a.SecretHeader
	if secretHeader == "" {
		secretHeader = "X-Client-Secret"
	}

	id := r.Header.Get(idHeader)
	secret := r.Header.Get(secretHeader)
	if id == "" || secret == "" {
		q := r.URL.Query()
		if id == "" {
			id = q.Get("id")
		}
		if secret == "" {
			secret = q.Get("secret")
		}
	}
	if id == "" || secret != a.Secret {
		return nil, "", ErrUnauthorized
	}
	return context.Background(), id, nil
}
