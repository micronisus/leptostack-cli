package config

// Redacted returns a copy of the config with secrets replaced, for
// `config view` and any default inspection output.
func (c *Config) Redacted() *Config {
	out := c.Clone()
	for _, cl := range out.Clusters {
		if cl == nil {
			continue
		}
		if cl.Git != nil && cl.Git.PAT != "" {
			cl.Git.PAT = "<redacted>"
		}
		if cl.Registry != nil && cl.Registry.Credentials != "" {
			cl.Registry.Credentials = "<redacted>"
		}
		if cl.Base != nil && cl.Base.PAT != "" {
			cl.Base.PAT = "<redacted>"
		}
	}
	return out
}
