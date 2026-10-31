# README demo, light version: open the TUI, search with filters, then
# download a book. The README picks this or docs/search.gif with
# <picture> and prefers-color-scheme, matching GitHub's light mode. The
# themes are the site's pair: catppuccin-latte (light), catppuccin-mocha (dark).
#
# Recorded against fixture data, never a real account: internal/cli/demo_test.go
# serves the same catalogue as the site's hero picker from an in-process fake
# EAPI, with a throwaway session, home directory (/home/you) and download
# folder (/home/books).
#
#   GOOS=linux go test -c ./internal/cli -o /tmp/zlib-demo
#   vhs docs/search-light.tape        # VHS_NO_SANDBOX=true when run as root
#
# Sized for a MacBook Retina display (2 device pixels per CSS pixel, 16:10).
# The README shows it at width="100%" of its content column, about 840 CSS
# pixels on a repository page, so 1680x1050 gives at least 2x density there
# and a clean downscale elsewhere. Font 22 is about 122x37 cells: room for
# 15 results under the book card.

Output docs/search-light.gif

Set Shell "bash"
Set Width 1680
Set Height 1050
Set FontSize 22
Set Framerate 30
Set Padding 24
Set WindowBar Colorful
Set TypingSpeed 90ms
# Catppuccin Latte with the exact background zlib's catppuccin-latte paints, so
# the padding around the terminal has no seam.
Set Theme { "name": "zlib-catppuccin-latte", "background": "#eff1f5", "foreground": "#4c4f69", "cursor": "#dc8a78", "selection": "#acb0be", "black": "#5c5f77", "red": "#d20f39", "green": "#40a02b", "yellow": "#df8e1d", "blue": "#1e66f5", "magenta": "#ea76cb", "cyan": "#179299", "white": "#acb0be", "brightBlack": "#6c6f85", "brightRed": "#d20f39", "brightGreen": "#40a02b", "brightYellow": "#df8e1d", "brightBlue": "#1e66f5", "brightMagenta": "#ea76cb", "brightCyan": "#179299", "brightWhite": "#bcc0cc" }

Hide
Type `zlib() { ZLIB_DEMO=1 ZLIB_DEMO_HOME=/home/you ZLIB_DEMO_DIR=/home/books ZLIB_THEME=catppuccin-latte ZLIB_DEMO_ARGS="$*" /tmp/zlib-demo -test.run '^TestDemo$'; }`
Enter
Type "cd /home/you && clear"
Enter
Sleep 300ms
Show

Source docs/search-steps.tape
