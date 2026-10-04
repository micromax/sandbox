// QuickJS REPL Driver
(function() {
    function decodeBase64Utf8(b64) {
        var bin = atob(b64);
        var bytes = [];
        for (var i = 0; i < bin.length; i++) bytes.push(bin.charCodeAt(i));
        var str = "";
        for (var i = 0; i < bytes.length;) {
            var c = bytes[i++];
            if (c < 0x80) str += String.fromCharCode(c);
            else if (c < 0xE0) str += String.fromCharCode(((c & 0x1F) << 6) | (bytes[i++] & 0x3F));
            else if (c < 0xF0) str += String.fromCharCode(((c & 0x0F) << 12) | ((bytes[i++] & 0x3F) << 6) | (bytes[i++] & 0x3F));
            else {
                var cp = ((c & 0x07) << 18) | ((bytes[i++] & 0x3F) << 12) | ((bytes[i++] & 0x3F) << 6) | (bytes[i++] & 0x3F);
                cp -= 0x10000;
                str += String.fromCharCode(0xD800 + (cp >> 10), 0xDC00 + (cp & 0x3FF));
            }
        }
        return str;
    }

    // Ready signal
    std.out.puts("READY\n");
    std.out.flush();

    while (true) {
        var token = std.in.getline();
        if (token === null) break;
        token = token.trim();
        if (token === "") continue;

        var b64 = std.in.getline();
        if (b64 === null) break;
        b64 = b64.trim();

        var code = "";
        try {
            code = decodeBase64Utf8(b64);
        } catch (e) {
            break;
        }

        var status = 0;
        try {
            var res = (0, eval)(code);
            if (res !== undefined) {
                console.log(res);
            }
        } catch (e) {
            if (e && e.stack) {
                std.err.puts(String(e) + "\n" + e.stack + "\n");
            } else {
                std.err.puts(String(e) + "\n");
            }
            status = 1;
        }

        std.err.flush();
        std.out.flush();
        std.err.puts("\n" + token + "\n");
        std.err.flush();
        std.out.puts("\n" + token + " EXIT:" + status + "\n");
        std.out.flush();
    }
})();
