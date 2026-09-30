# Programme image samples

`programme-images.json` contains selected real metadata rows and titles from
the supplied, token-blanked EpGo cache. The rows retain the preferred aspect
at several sizes; the movie also retains untiered Iconic art for its backdrop.
No account fields or tokens are included.

- `MV000371790000`: The Fugitive (movie Poster Art and untiered Iconic).
- `EP002061390830`: Deutschlandbilder (Episode and Season art).
- `SH000199170000`: SportsCenter (only Series art).
- `EP000021447124`: Horse Racing (only Sport Event art).
- `EP000031285726`: NFL Football (Team Event Backdrop-Sports art).

The source cache has no EP-prefixed metadata entries with Series-tier art.
The series-only episode test therefore reuses the real SportsCenter image
list under a synthetic EP ID; the source sample remains unchanged.

Tests use an in-memory HTTP transport, never an SD API or image server.
The XML sample uses production `getProgram`/`Programme` serialization with
synthetic airtimes and a minimal `<tv>` wrapper. It verifies programme image
placement, not strict DTD compliance of the whole `CreateXMLTV` output: existing
`EPGo` root attributes, channel `<lcn>` and programme `<live>` extensions are
outside the supplied DTD and are unchanged here.
Optional offline verification:

```sh
EPGO_IMAGE_FIXTURE=/path/to/cache.json go test -run TestImageFixtureStatistics -v
EPGO_XMLTV_SAMPLE=/path/to/sample.xml go test -run TestProgrammeImagesXML -v
xmllint --noout --dtdvalid /path/to/xmltv.dtd /path/to/sample.xml
```
