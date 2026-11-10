# Demo covers

Real edition covers from Open Library, used only as offline recording fixtures.
Cover artwork belongs to its respective rights holders; it is not project artwork.

| Fixture | Book | Source |
| --- | --- | --- |
| 115066162.jpg | Dune | https://covers.openlibrary.org/b/isbn/9780441172719-M.jpg |
| 27462851.jpg | Dune Messiah | https://covers.openlibrary.org/b/isbn/9780441172696-M.jpg |
| 19288013.jpg | God Emperor of Dune | https://covers.openlibrary.org/b/isbn/9780441294671-M.jpg |
| 88214470.jpg | Chapterhouse: Dune | https://covers.openlibrary.org/b/isbn/9780441102679-M.jpg |

The demo seeds the normal cover cache with these images. Production cover
fetching and terminal rendering remain unchanged.

VHS 0.12.1 captures only the text and cursor canvases by default, omitting
the Sixel image layer. For cover recordings, the recorder must composite
the image canvas from `.xterm-screen` over the text canvas before exporting
each frame. Do not overlay cover artwork afterwards: capture the images
rendered by the running terminal, including selection changes and removal
when a dialog opens.
