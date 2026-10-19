#!/usr/bin/env python3
# Types each command below in vim itself and writes the text and cursor it
# leaves to cmd/mayoi/testdata/vim.json, which TestVimLikeVim checks vim
# mode against. Run it after adding a command, with vim on the PATH.
# Usage: scripts/vim-cases.py
#
# Keys are as vim's :normal reads them, with <Esc>, <CR>, <BS> and <C-x>.
# Leave out commands whose vim result is :normal's rather than the
# editor's: a motion that fails ends the whole command, and changes made
# in one :normal undo as one.
import json
import os
import re
import subprocess
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "cmd", "mayoi", "testdata", "vim.json")

T1 = "one two.three  four\n  five (six [x]) end\n\nlast line\n"
T2 = "alpha beta gamma\nsecond line\nthird\n"
T3 = "func f(a, b) {\n    if a {\n        return \"x y\"\n    }\n    return 'q'\n}\n"
T4 = "foo_bar baz-qux\n\n\n  indented line\nend\n"
T5 = "a\nb\nc\nd\ne\n"
keys = {
 T1: ["w","3w","W","e","3e","E","b","$b","ge","gE","$","0","^","g_","j$","2$","G","gg","2G","}","2}","{","f(%","%","fxdt]","dfe","dte","cwX<Esc>","cWX<Esc>","ceX<Esc>","dw","d2w","2dw","dW","de","dE","db","$db","d0","$d^","d$","D","2D","C<Esc>","cc<Esc>","dd","2dd","3dd","dj","dk","jdk","dG","jdgg","yyp","yyP","yy2p","yiwP","yiwp","x","5x","$x","X","$X","3X","s<Esc>","Sz<Esc>","rz","3rz","r<CR>","~","5~","g~w","guu","gUU","gUiw","J","3J","jJ","Jx","o<Esc>","Oz<Esc>","ix<Esc>","ax<Esc>","Ix<Esc>","Ax<Esc>","jIx<Esc>","A<Esc>","ix<Esc>.","dw.","dd.","x..","3x.","2.","ciwZ<Esc>w.","diw","daw","diW","daW","wdaw","$daw","jf(di(","jf(da(","jfsdi(","jf[di[","jf[da]","jfxci[Q<Esc>","jf)di)","jdib","djp","ddp","ddP","yyjp","dwP","xp","ddjp","jj","jjj","jjjk","3j","kk","10j","l","5l","$l","h","3h","+","-","j-","<CR>","2<CR>","wv","vd","vey","vep","wviwd","Vd","Vjd","VjJ","vjd","vj$d","Vy","vlld","vllc_<Esc>","vllo","vlloh","v3lr-","Vr-","vU","vjU","Vu","vj~","v$d","v$y","vee","ve<Esc>","Vjy","Vjc_<Esc>","vjjd","Vjjx","vX","vD","vC_<Esc>","vS_<Esc>","vY","yeP","yep","yw$p","y$P","y0","wy0P","vlyjp","Vyjp","VyjP","Vjyp","ywvep","dd.","ddu","xxuu","ix<Esc>u","cwab<Esc>u","Oab<Esc>u","2ddu",";",",","fo;",";;","fo,","to;","t ;","Fo","3fo","dfo","ctoX<Esc>","dto","dFo","$dFo","$dTo","yfo","c$X<Esc>","cjX<Esc>","5j","G5k","3gg","100G","dgg","yGP"],
 T2: ["w.", "dwj.", "ciwX<Esc>j.", "ix<Esc>j.", "Ax<Esc>j.", "2dw", "3x", "A<BS><Esc>", "e", "eee", "ea<Esc>", "cw<Esc>", "cwx<Esc>w.", "J.", "yyjjp", "yiwjviwp", "$v0d", "v$y$p", "Vjjd", "jVkd", "3Vd", "vjy", "2yy", "2yyjp", "y2y", "dap", "dip", "yip"],
 T3: ["jjf\"di\"", "jjf\"da\"", "jjfxci\"Q<Esc>", "jjdi\"", "4jfqdi'", "4jfqca'Z<Esc>", "jdi{", "jda{", "jjjdi{", "di(", "f(ci(z<Esc>", "di{", "jdiB", "f{%", "G%", "jjjj%", "f,%", "jjo<Esc>", "jo<Esc>", "jO<Esc>", "jjox<Esc>", "jjOx<Esc>", "jjS<Esc>", "jjSx<Esc>", "jjccx<Esc>", "jjcc<Esc>", "jjJ", "jjVjJ", "w", "W", "3W", "jjw", "jj$b", "jjf\"yi\"P", "2jci\"<Esc>", "jj^D", "jj^d$", "jj^y$P", "jjvi\"d", "jjva\"d", "jvi{d", "jva{d", "jvi{y", "jjviwd", "fadaw", "dtb", "f{di}", "f{yi}", "jjjd%", "f(d%", "f(y%P", "ct(X<Esc>"],
 T4: ["w", "ww", "www", "wwww", "e", "ee", "eee", "eeee", "b", "Gb", "Gbb", "Gbbb", "Gge", "Ggege", "W", "WW", "E", "EE", "}", "}}", "}}}", "G{", "G{{", "dw", "d3w", "dw.", "cw_<Esc>", "c3w_<Esc>", "jdd", "jjdd", "jj2dd", "J", "jJ", "3J", "jjjJ", "dap", "jjjdap", "dip", "jdip", "wciwZ<Esc>", "wdiw", "wdaw", "wdiW", "wdaW", "jdiw", "jjjdiw", "jjjwdaw", "jjjdaw", "jjj^daw", "Gdaw", "Gdiw", "dd", "jjjdd", "jjjyyGp", "Gyykp", "jjj0d^", "jjj^", "jjj0w"],
 T5: ["dd", "Gdd", "Gkdd", "2dd", "Gdk", "dj", "Gdj", "ddp", "Gddp", "GddP", "yyp", "Gyyp", "ggyyGp", "3J", "Gkd2j", "jjdG", "jjdgg", "jjyGP", "jjyggP", "5x", "Gx", "Gp", "o<Esc>", "Go<Esc>", "GO<Esc>", "jjVd", "GVkd", "Vjjd", "V2jy", "VGd", "Vggd", "GVggd", "jjVjp", "yyjVp", "yyGVp", "xjp", "xjP", "jjxGp", "u", "<C-r>", "dddd", "dd2u", "dddduu"],
}
T6 = "if x {\n    a()\n\n    b()\n}\n\nfunc g() {\n    c()\n}\n"
T7 = "  lead \"q\" tail\nx = 'a' + 'b'\n"
keys[T6] = ["d}", "jd}", "jjjd}", "wd}", "y}P", "c}z<Esc>", "jdip", "jdap", "jjdap", "jjdip", "jyipP", "jvipd", "jvapd", "Gdap", "Gdip", "jd2ap", "2dap", "jcipz<Esc>", "j>ip", "di{", "jjjdi{", "Gdi{", "d{", "Gd{", "jjjd{", "jjjy{P", "jjjvi{d", "f{%d%", "jdj", "jd}u", "3jo<Esc>", "jo<Esc>.", "jcc<Esc>", "jS<Esc>", "jO<Esc>j.", "jox<Esc>", "jj2D", "jjD", "j^2D", "j$2D", "j0wd}", "jwdj", "jwd2j", "r<CR>", "jr<CR>", "w2r<CR>", "v$y$p", "Vjjd", "jv$d", "jv$jd", "j$vjd", "3vd", "2Vd", "3Vy", "jvj$y", "j3vU", "yiwwvep", "yiwwveP", "yiwwvePwvep", "Vjdj.", "vlldj.", "vjdj.", "Vj>jjj.", "vecz<Esc>w.", "v~w.", "vjrxj.", "vjlyGp", "jVyGp", "wvlcab<Esc>W."]
keys[T7] = ["da\"", "di\"", "ca\"z<Esc>", "jda'", "jfbda'", "jdi'", "jfbci'z<Esc>", "di(", "ci[z<Esc>", "jdib", "jdab"]

T8 = "alpha beta\nGamma alpha\n  delta beta x1\nlast alpha 9\n"
T9 = "zeta\nalpha 10\nbeta 2\nalpha 10\ngamma -3\n"
keys[T8] = ["/beta<CR>", "/beta<CR>n", "/beta<CR>nn", "/beta<CR>N", "?alpha<CR>", "?alpha<CR>n", "G?beta<CR>", "/gam<CR>",
    "/Gam<CR>", "/a.p<CR>", "/\\<beta\\><CR>", "/al\\|de<CR>n", "/x\\d<CR>", "/[gd]<CR>", "/a\\+<CR>", "/\\(be\\)ta<CR>",
    "2/alpha<CR>", "/alpha<CR>2n", "d/beta<CR>", "y/de<CR>P", "c/Gam<CR>z<Esc>", "jd?beta<CR>", "v/del<CR>d", "*", "w*", "#",
    "w#", "**", "jw*", "/beta<CR>/<CR>", "/beta<CR>dn", "/beta<CR>u", ":2<CR>", ":$<CR>", ":3<CR>", ":+2<CR>", ":2,3d<CR>",
    ":%d<CR>", ":.,$d<CR>", "j:d<CR>", ":s/a/A/<CR>", ":s/a/A/g<CR>", ":%s/alpha/X/<CR>", ":%s/a/_/g<CR>",
    ":%s/\\(\\w\\+\\) \\(\\w\\+\\)/\\2 \\1/<CR>", ":%s/beta/[&]/<CR>", ":%s/ /\\r/<CR>", ":%s#a#/#g<CR>",
    ":%s/x1/x\\/2/<CR>", ":%s/alpha/A/gi<CR>", "jVj:s/a/@/g<CR>", "Vj:d<CR>", ":2,3s/^/# /<CR>", ":%s/$/;/<CR>",
    ":%s/beta//g<CR>", "/beta<CR>:s//B/<CR>", ":%s/alpha/A/<CR>u", ":2,3j<CR>", ":j<CR>", ":2,3y<CR>P",
    "jj:<<CR>", "3:d<CR>", "gJ", "jgJ", "3gJ", "jj$<C-a>", "jj<C-a>", "jj5<C-a>", "jj<C-x>", "jj12<C-x>", "G<C-a>",
    "G$<C-x>", "G10<C-x>", "vjy0gvd", "vey$gvd", "Vjyjgvd", "vjd.gvy", "vjy:'<,'>d<CR>"]
keys[T9] = [":sort<CR>", ":sort!<CR>", ":sort u<CR>", ":sort n<CR>", ":2,4sort<CR>", "Vjj:sort<CR>", ":sort i<CR>"]

# Counts before an insert, registers, marks, and r<CR> in an indent.
keys[T1] += ["3ix<Esc>", "3ax<Esc>", "3Ax<Esc>", "3Ix<Esc>", "2ox<Esc>", "2Ox<Esc>", "3ix<Esc>.", "3ix<Esc>j.",
    "2Ax<Esc>j.", "2ox<Esc>.", "3iab<Esc>", "2a<CR><Esc>", "\"ayy\"ap", "\"ayyj\"aP", "\"ayw\"bywj\"ap\"bp",
    "\"Ayy\"ap", "\"ayyj\"Ayy\"ap", "\"ayw\"Aywj\"aP", "\"_ddp", "yyj\"_ddp", "yiwdd\"0P", "dddd\"1p\"2p",
    "dw\"-P", "\"add\"ap", "3\"ayy\"ap", "\"a3yy\"ap", "\"ay$\"ap", "wmajj`ax", "wmajj'ax", "jmakd'a",
    "jjmaggy'ap", "wmabd`a", "jmaggd`a", "Gmagg``x", "G''x", "Gggx``x", "/two<CR>``x", "jmaG'a''x", "yyjgpx",
    "yiwgPx", "yyjgPx", "jf(dib", "jfxd[(", "jfxd])", "jfx[(x", "jfx])x", "jfxc[(z<Esc>"]
T10 = "<ul>\n  <li class=\"a\">one <b>two</b></li>\n  <li>three</li>\n</ul>\n"
T11 = "f(a, g(b, c), d)\nif (x) { y(); }\n"
T12 = "1 one\n2 two\n3 three\n4 four\n5 five\n6 six\n"
keys[T3] += ["jjf\"r<CR>", "jjfyr<CR>"]
keys[T2] += ["ixy<Esc>\".p", ":s/a/b/<CR>j&", ":s/a/b/g<CR>j&", ":s/a/b/<CR>g&", ":2,3m0<CR>", ":1m$<CR>",
    ":1m2<CR>", ":3m1<CR>", ":1t.<CR>", ":1,2t$<CR>", ":3t0<CR>", ":1co2<CR>", "yy:3pu<CR>", "yy:pu!<CR>",
    ":%norm Ax<CR>", ":%norm! Ix<CR>", ":2,3norm dw<CR>", ":norm 2x<CR>", ":%norm $x<CR>", ":g/a/d<CR>",
    ":v/a/d<CR>", ":g!/a/d<CR>", ":g/i/s/i/I/<CR>", ":g/a/norm Ax<CR>", ":g/e/m0<CR>", ":2,3g/./d<CR>",
    ":g/third/d<CR>", ":g/alpha/d<CR>u"]
keys[T5] += [":g/[bd]/d<CR>", ":g/./m0<CR>", ":g/./t.<CR>", ":%norm Ax<CR>u", "dd:g/c/norm dd<CR>",
    ":2,4norm Ax<CR>", "\"ayyj\"byyG\"ap\"bp"]
keys[T10] = ["jfodit", "jfodat", "jfociti<Esc>", "jfwdit", "jfwd2it", "jfwdat", "jjditkdit", "dit", "jfwyitP",
    "jfwvitd", "jfwvatd", "jjfhcitX<Esc>", "jfw2dit", "jdit"]
keys[T11] = ["fbd[(", "fbd])", "fbc[(z<Esc>", "fb[(x", "fb])x", "fb2[(x", "jfyd[{", "jfyd]}", "jfy[{x", "jfy]}x",
    "fad])", "fd[(x", "f,[(x"]
keys[T12] = [":2,4norm Ax<CR>", ":g/[24]/d<CR>", ":g/o/norm A!<CR>", ":v/o/d<CR>", ":2,5m0<CR>", ":4,5m1<CR>",
    ":2m4<CR>", ":5m2<CR>", ":2,3t5<CR>", ":g/^/m0<CR>", ":g/e$/s/e/E/<CR>", "3Gma5Gd'a", "3Gma5Gy'aP",
    "3Gmb:'b,$d<CR>", "jVj:norm Ax<CR>"]


def vim_keys(keys):
    out = keys.replace("\\", "\\\\").replace('"', '\\"')
    return re.sub(r"<(Esc|CR|BS|Tab|C-[a-z])>", lambda m: "\\<" + m.group(1) + ">", out)


def run(text, keys):
    with tempfile.TemporaryDirectory() as d:
        path, pos = os.path.join(d, "a.txt"), os.path.join(d, "pos")
        with open(path, "w") as f:
            f.write(text)
        subprocess.run([
            "vim", "-u", "NONE", "-i", "NONE", "-N", "-n", "-Es",
            "-c", "set autoindent shiftwidth=4 expandtab tabstop=4 nojoinspaces ignorecase smartcase",
            "-c", "call cursor(1, 1)",
            "-c", 'exe "normal! ' + vim_keys(keys) + '"',
            "-c", 'call writefile([line2byte(line(".")) + col(".") - 2], "' + pos + '")',
            "-c", "w!", "-c", "qa!", path,
        ], timeout=10, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, check=False)
        with open(path) as f:
            want = f.read()
        with open(pos) as f:
            cursor = int(f.read().strip())
    return want, cursor


cases = []
for text, ks in keys.items():
    for k in ks:
        want, cursor = run(text, k)
        cases.append({"text": text, "keys": k, "want": want, "cursor": cursor})
with open(OUT, "w") as f:
    json.dump(cases, f, indent=1)
    f.write("\n")
print(f"{len(cases)} commands in {os.path.relpath(OUT, ROOT)}")
