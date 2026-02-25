import sys

with open('pkg/server/idle.go', 'r') as f:
    content = f.read()

target = '''	go func() {
		for {
			if err := display.Context().GetDispatch()(); err != nil {
				return
			}
		}
	}()'''

new_code = '''	go func() {
		for {
			dispatchFunc := display.Context().GetDispatch()
			// We MUST NOT hold im.wlMu while executing dispatchFunc,
			// because handlers (like idledHandler) might try to do something,
			// though currently they just set a boolean or close a channel.
			if err := dispatchFunc(); err != nil {
				return
			}
		}
	}()'''

if target in content:
    content = content.replace(target, new_code)
else:
    print("Target not found")
    sys.exit(1)

with open('pkg/server/idle.go', 'w') as f:
    f.write(content)
