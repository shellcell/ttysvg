# bash completion for ttysvg

_ttysvg()
{
    local cur prev opts themes

    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    opts="-o -size -frame -idle -font-size -font-family -cell-width -cell-height -padding -theme -bg -minify -no-query-terminal -no-clear -version -q -h -help --"
    themes="auto dark light"

    case "$prev" in
        -o)
            COMPREPLY=( $(compgen -f -- "$cur") )
            return 0
            ;;
        -theme)
            COMPREPLY=( $(compgen -W "$themes" -- "$cur") )
            return 0
            ;;
        -size|-frame|-idle|-font-size|-font-family|-cell-width|-cell-height|-padding|-bg)
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
