package config

// Redacted returns a copy of the config with secrets replaced, for
// `config view` and any default inspection output.
func (c *Config) Redacted() *Config {
	out := c.Clone()
	if out.Git != nil && out.Git.PAT != "" {
		out.Git.PAT = "<redacted>"
	}
	if out.Registry != nil && out.Registry.Credentials != "" {
		out.Registry.Credentials = "<redacted>"
	}
	if out.Base != nil && out.Base.PAT != "" {
		out.Base.PAT = "<redacted>"
	}
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

// SetGitPAT sets the global Git PAT.
func (c *Config) SetGitPAT(v string) {
	if c.Git == nil {
		c.Git = &Git{}
	}
	c.Git.PAT = v
}

// SetRegistryCredentials sets the global registry credentials.
func (c *Config) SetRegistryCredentials(v string) {
	if c.Registry == nil {
		c.Registry = &Registry{}
	}
	c.Registry.Credentials = v
}
