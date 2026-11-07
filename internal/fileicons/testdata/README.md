`seti-subset.woff2` is VS Code's `seti.woff` (from `bundled/seti.vsix`, MIT
License) cut down to U+E001 and U+E01D and saved as WOFF2, with fontTools:

    pyftsubset seti.woff --unicodes=U+E001,U+E01D --flavor=woff2 --output-file=seti-subset.woff2
