# Credits and Acknowledgements

zarfs would not exist without the work of many people in the RISC OS
community who designed, documented, and implemented the archive formats
supported here.

## Archive Format Authors

- **David Pilling** created the Spark archive format in 1988 and
  SparkFS in 1992. SparkFS is now open source, maintained by RISC OS
  Open Ltd. David's [SparkFS page](https://www.davidpilling.com/wiki/index.php/SparkFS)
  contains invaluable history and technical context.

- **Mark Smith** wrote ArcFS, the first RISC OS archive filing system,
  published by The ARM Club in 1991. The free read-only version of
  [ArcFS](https://armclub.org.uk/products/arcfs/) became ubiquitous on
  RISC OS magazine cover discs.

- **John Kortink** wrote PackDir, a fast tree-structured archiver for
  RISC OS using Zoo-variant LZW compression.

- **Acorn Computers** created the Squash module and application,
  providing built-in 12-bit LZW compression via the SWI interface. The
  [Squash source](https://gitlab.riscosopen.org/RiscOS/Sources/Lib/Squash)
  is now available from RISC OS Open Ltd.

- **Computer Concepts** created CFS (Compacted File System), a
  transparent compressed filing system for RISC OS.

## Reference Implementations

zarfs's archive parsers and decompression algorithms were developed
with reference to:

- **riscosarc** by James Woodcock: a Java implementation of
  de-archivers for all five supported formats. MIT licence.
  [github.com/mjwoodcock/riscosarc](https://github.com/mjwoodcock/riscosarc)

- **nspark** by Andy Duplain (based on David Pilling's "bark"): a C
  de-archiver for Spark and ArcFS archives, packaged in Debian/Ubuntu.
  Free software ("do what you like with it" licence).
  [github.com/mjwoodcock/nspark](https://github.com/mjwoodcock/nspark)

- **ncompress** by Peter Jannesen and others: the canonical Unix
  `compress` implementation, from which the LZW bit-reading algorithm
  is derived. Public domain.
  [github.com/vapier/ncompress](https://github.com/vapier/ncompress)

## Test Data

The test archive `testdata/arcfs.arc` is the freely distributable
read-only version of ArcFS, downloaded from
[armclub.org.uk](https://armclub.org.uk/products/arcfs/). It is
copyright The ARM Club and redistributed under the terms stated on
their website.

## Standards Documentation

Format specifications are drawn from:

- [RISC OS Programmer's Reference Manuals](https://www.riscos.com/support/developers/prm/)
  (Acorn/RISC OS Open Ltd)
- [spark(5)](https://manpages.ubuntu.com/manpages/noble/man5/spark.5.html)
  and [arcfs(5)](https://manpages.ubuntu.com/manpages/noble/man5/arcfs.5.html)
  man pages (from nspark)
- [Archive Team File Format Wiki](http://fileformats.archiveteam.org/)
  entries for Spark, ArcFS, PackDir, Squash, and CFS
- [David Pilling's SparkFS manual](https://www.davidpilling.com/software/SparkFS.pdf)
