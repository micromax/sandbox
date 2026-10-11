import six

print("Six version:", six.__version__)
print("six.PY3:", six.PY3)
print("six.string_types:", six.string_types)
print("Checking isinstance('test', six.string_types):", isinstance("test", six.string_types))

# Test dictionary iteration helpers
d = {"a": 1, "b": 2, "c": 3}
keys = list(six.iterkeys(d))
print("Iterated keys using six:", keys)
print("Status: SUCCESS - Package loaded and executed cleanly in sandbox!")
