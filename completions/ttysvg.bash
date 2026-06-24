# bash completion for ttysvg

_ttysvg()
{
    local cur prev opts themes bools

    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    opts="-o -size -frame-ms -idle-ms -font-size -font-family -cell-width -cell-height -padding -theme -minify -query-terminal -query-terminal=true -query-terminal=false -clear -clear=true -clear=false -version -q -h -help --"
    themes="auto dark light"
    bools="true false"

    case "$prev" in
        -o)
            COMPREPLY=( $(compgen -f -- "$cur") )
            return 0
            ;;
        -theme)
            COMPREPLY=( $(compgen -W "$themes" -- "$cur") )
            return 0
            ;;
        -query-terminal|-clear)
            COMPREPLY=( $(compgen -W "$bools" -- "$cur") )
            return 0
            ;;
        -size|-frame-ms|-idle-ms|-font-size|-font-family|-cell-width|-cell-height|-padding)
            return 0
            ;;
        --)
            COMPREPLY=( $(compgen -c -- "$cur") )
            return 0
            ;;
    esac

    if [[ "$cur" == -* ]]; then
        COMPREPLY=( $(compgen -W "$opts" -- "$cur") )
        return 0
    fi

    COMPREPLY=( $(compgen -c -- "$cur") )
}

complete -F _ttysvg ttysvg
