# bash completion for ttysvg

_ttysvg()
{
    local cur prev opts themes

    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    opts="-o -gz -size -fps -frame -idle -font-size -font-family -cell-width -cell-height -padding -theme -bg -query-terminal -no-clear -autostart -headless -no-loop -hold -cast -version -q -h -help --"
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
        -size|-fps|-frame|-idle|-font-size|-font-family|-cell-width|-cell-height|-padding|-bg|-hold)
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
