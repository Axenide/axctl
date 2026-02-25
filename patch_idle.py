import sys

with open('pkg/server/idle.go', 'r') as f:
    content = f.read()

target = '''	<-done'''

new_code = '''	// Wait for the sync callback to finish
	// We CANNOT block here indefinitely if the background dispatch is blocked elsewhere
	// The background dispatch loop handles events. It will close 'done' when Sync finishes.
	<-done'''

if target in content:
    content = content.replace(target, new_code)
else:
    print("Target not found")

with open('pkg/server/idle.go', 'w') as f:
    f.write(content)
