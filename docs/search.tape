# README demo, dark version: open the TUI, search with filters, then
# download a book. The README picks this or docs/search-light.gif with
# <picture> and prefers-color-scheme, matching GitHub's dark mode. The
# themes are the site's pair: catppuccin-latte (light), catppuccin-mocha (dark).
#
# Recorded against fixture data, never a real account: internal/cli/demo_test.go
# serves the same catalogue as the site's hero picker from an in-process fake
# EAPI, with a throwaway session, home directory (/home/you) and download
# folder (/home/books).
#
#   GOOS=linux go test -c ./internal/cli -o /tmp/zlib-demo
#   vhs docs/search.tape        # VHS_NO_SANDBOX=true when run as root
#
# Sized for a MacBook Retina display (2 device pixels per CSS pixel, 16:10).
# The README shows it at width="100%" of its content column, about 840 CSS
# pixels on a repository page, so 1680x1050 gives at least 2x density there
# and a clean downscale elsewhere. Font 22 is about 122x37 cells: room for
# 15 results under the book card.

Output docs/search.gif

Set Shell "bash"
Set Width 1680
Set Height 1050
Set FontSize 22
Set Framerate 30
Set Padding 24
Set WindowBar Colorful
Set TypingSpeed 90ms
# Catppuccin Mocha with the exact background zlib's catppuccin-mocha paints, so
# the padding around the terminal has no seam.
Set Theme { "name": "zlib-catppuccin-mocha", "background": "#1e1e2e", "foreground": "#cdd6f4", "cursor": "#f5e0dc", "selection": "#585b70", "black": "#45475a", "red": "#f38ba8", "green": "#a6e3a1", "yellow": "#f9e2af", "blue": "#89b4fa", "magenta": "#f5c2e7", "cyan": "#94e2d5", "white": "#bac2de", "brightBlack": "#585b70", "brightRed": "#f38ba8", "brightGreen": "#a6e3a1", "brightYellow": "#f9e2af", "brightBlue": "#89b4fa", "brightMagenta": "#f5c2e7", "brightCyan": "#94e2d5", "brightWhite": "#a6adc8" }

Hide
Type `zlib() { ZLIB_DEMO=1 ZLIB_DEMO_HOME=/home/you ZLIB_DEMO_DIR=/home/books ZLIB_THEME=catppuccin-mocha ZLIB_DEMO_ARGS="$*" /tmp/zlib-demo -test.run '^TestDemo$'; }`
Enter
Type "cd /home/you && clear"
Enter
Sleep 300ms
Show

Source docs/search-steps.tape
