# Shared steps for docs/search.tape (dark) and
# docs/search-light.tape (light). Edit the flow here, so both GIFs
# always show the same thing.

Sleep 600ms
Type "zlib tui"
Sleep 400ms
Enter
Sleep 1.5s

# Open the search form from the command palette
Type "/search"
Sleep 700ms
Enter
Sleep 900ms

# Query
Type "Dune"
Sleep 600ms

# Languages: open the list, keep the default (any language)
Tab
Sleep 150ms
Tab
Sleep 150ms
Tab
Sleep 500ms
Enter
Sleep 1.4s
Escape
Sleep 500ms

# Formats: EPUB only
Tab
Sleep 400ms
Enter
Sleep 800ms
Space
Sleep 900ms
Escape
Sleep 900ms

# Sort by: open the list, keep the default
Tab
Sleep 250ms
Tab
Sleep 500ms
Enter
Sleep 1.4s
Escape
Sleep 700ms

# Search
Ctrl+S
Sleep 1.8s

# Look through the results; the card follows the selection
Down
Sleep 600ms
Down
Sleep 600ms
Down
Sleep 800ms
Up
Sleep 300ms
Up
Sleep 300ms
Up
Sleep 800ms

# Download "Dune" to the default folder
Enter
Sleep 900ms
Right
Sleep 500ms
Enter
Sleep 1.1s
Enter
Sleep 4s
