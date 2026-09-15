# Test carts

Real PICO-8 carts the test suite runs against. Each cart keeps the license it
came with; the repository's own MIT license does not extend to them.

## From picotool

`test_cart.p8.png`, `test_gol.p8.png` and `empty.p8.png`, along with the `.p8`
text of each, come from [dansanderson/picotool](https://github.com/dansanderson/picotool)
at commit `49808e5ddb21e886a5608a19c95ec3ca6f1ee541`, MIT licensed.

The `.p8` files are the sources those carts were built from, so they double as
reference output: an independent record of what each cart's sections should
contain. `test_cart.p8` matches this tool's conversion byte for byte.
`test_gol.p8` and `empty.p8` differ only in trailing blank lines - a cart
round-trip drops trailing whitespace from the source, and PICO-8 is itself
inconsistent about the blank line before `__gff__` - so those two are compared
section by section with trailing blank lines ignored.

## Celeste

`celeste.p8.png` is Celeste by Maddy Thorson and Noel Berry, published on the
Lexaloffle BBS under CC BY-NC-SA 4.0. It is the only large real game in the
suite, and the only version 5 cart, which is what makes it worth keeping.

There is no reference `.p8` for it, so its conversion is pinned by
`celeste.p8.sha256` plus assertions about the shape of the output. Regenerate
the checksum with:

```bash
go run . testdata/celeste.p8.png /tmp/celeste.p8
shasum -a 256 /tmp/celeste.p8 | awk '{print $1}' > testdata/celeste.p8.sha256
```

Only do that when the conversion is meant to change, and say why in the commit.
