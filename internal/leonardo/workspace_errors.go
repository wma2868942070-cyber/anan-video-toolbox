package leonardo

import "errors"

// ErrAccountWorkspaceAlreadyBound means the Cookie was read and validated,
// but another managed Chrome workspace already owns recovery for the same
// stable Leonardo account. The account data may still be safely refreshed;
// only the duplicate recovery-workspace binding must be skipped.
var ErrAccountWorkspaceAlreadyBound = errors.New("Leonardo 账号已绑定其他工作区")
