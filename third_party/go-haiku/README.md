# Patched go-haiku

Local copy of github.com/0x307e/go-haiku with:

- UniDic `接頭辞` allowed as a phrase start (same as IPADic `接頭詞`)
- `FindWithOpt` debug output when `Opt.Debug` is set

Wired via `replace` in the root `go.mod`. Remove this directory and the replace once the changes are merged upstream.
