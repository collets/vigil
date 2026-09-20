# Adapter fixture

Change `message.txt` to exactly `adapter spike ready` followed by a newline.
Do not change other files. You may read files and run `sh check.sh`.
Do not commit, create branches, use network access, delegate, or start background jobs.
Finish with JSON: `{"summary":"Updated message.txt","files":["message.txt"]}`.

The controller independently checks the file and diff. A successful harness reply
is an execution outcome only; this fixture does not implement product acceptance.
