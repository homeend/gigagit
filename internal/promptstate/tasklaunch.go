package promptstate

// TaskLaunch is the AI-task launch dialog's last choice for one task kind
// (spec ruling 1: the dialog preselects it; enter repeats it).
type TaskLaunch struct {
	Agent   string `toml:"agent"`   // "id:<tool id>" or "name:<command name>"
	Mode    string `toml:"mode"`    // background | foreground | headless
	Command string `toml:"command"` // the command's name: picks the variant
}

// TaskLaunchChoice returns the remembered choice for kind.
func (fs *FileStore) TaskLaunchChoice(kind string) (TaskLaunch, bool) {
	c, ok := fs.read().TaskLaunch[kind]
	return c, ok
}

// SetTaskLaunchChoice remembers c for kind.
func (fs *FileStore) SetTaskLaunchChoice(kind string, c TaskLaunch) error {
	r := fs.read() // read-merge: pick up any sibling writes first
	if r.TaskLaunch == nil {
		r.TaskLaunch = map[string]TaskLaunch{}
	}
	r.TaskLaunch[kind] = c
	return fs.write(r)
}
