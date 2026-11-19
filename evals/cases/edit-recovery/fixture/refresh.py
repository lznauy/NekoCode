from pathlib import Path

path = Path("settings.ini")
text = path.read_text()
if not text.startswith("# generated v2\n"):
    path.write_text("# generated v2\n" + text)
