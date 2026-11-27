# Preview navigation

- A successful operator HTTP response does not prove management navigation. The first preview advertised port 8788 without starting its management backend; that port belonged to another application.
- Use explicit runtime ports, isolate the preview backend, and verify actual catalog-to-editor-to-operator transitions in Chromium before sharing a preview link. Never stop an unrelated listener to make a hardcoded link work.
- A forwarded preview origin is not the server's listen address. Use one browser origin for all preview navigation and API requests; validate a forwarded Host and real saved data rather than only inspecting link hostnames.
