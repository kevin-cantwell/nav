### Note:
Pretty much the same functionality (maybe even better) can be achieved with a short `fd` + `fzf` script:

```
cdi() {
    root=`git rev-parse --show-toplevel 2>/dev/null || echo $HOME`
    newdir=`{ echo $root; fd -t d . $root; } | fzf --layout=reverse || echo '.'`
    cd $newdir
}
```


# nav
A cli for quickly finding project directories with fuzzy matching

# Usage

```
go get -u github.com/kevin-cantwell/nav
alias cdi='cd "$(nav)"'

cd $PROJECT_DIR
cdi
```

This will start a terminal gui that is fairly self-explanatory.
