package ssh

type Logger interface {
	Debugw(msg string, kv ...interface{})
}
