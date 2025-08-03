# Shell Autocomplete

To generate a shell completion script, invoke pilreg with `--autocomplete` and your shell name (bash, zsh, fish or PowerShell). For example:

```bash
# Bash:
pilreg --autocomplete bash > /etc/bash_completion.d/pilreg

# Zsh (Oh My Zsh):
pilreg --autocomplete zsh > ${fpath[1]}/_pilreg

# Fish:
pilreg --autocomplete fish | source

# PowerShell:
pilreg --autocomplete powershell > pilreg.ps1
```
