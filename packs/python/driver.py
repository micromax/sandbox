import sys, base64, traceback

def _driver_main():
    user_globals = {
        '__name__': '__main__',
        '__doc__': None,
        '__builtins__': __builtins__,
    }

    # Ready signal
    sys.stdout.write("READY\n")
    sys.stdout.flush()

    while True:
        token_line = sys.stdin.readline()
        if not token_line:
            break
        token = token_line.strip()
        if not token:
            continue

        b64_line = sys.stdin.readline()
        if not b64_line:
            break

        try:
            code = base64.b64decode(b64_line.strip()).decode('utf-8')
        except Exception:
            break

        status = 0
        try:
            # Try to compile as eval first to display expression results
            try:
                compiled = compile(code, '<session>', 'eval')
                is_expr = True
            except SyntaxError:
                compiled = compile(code, '<session>', 'exec')
                is_expr = False

            if is_expr:
                res = eval(compiled, user_globals)
                if res is not None:
                    print(repr(res))
            else:
                exec(compiled, user_globals)
        except SystemExit as e:
            status = e.code if isinstance(e.code, int) else (1 if e.code else 0)
        except BaseException:
            traceback.print_exc(file=sys.stderr)
            status = 1

        sys.stderr.flush()
        sys.stdout.flush()
        sys.stderr.write('\n' + token + '\n')
        sys.stderr.flush()
        sys.stdout.write('\n' + token + ' EXIT:' + str(status) + '\n')
        sys.stdout.flush()

if __name__ == '__main__':
    _driver_main()
