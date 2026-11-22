"""Minimal build CLI for testing the field-atlas recursive fallback probe."""
import json
import os
from pathlib import Path
import sys


def build(src, dest):
    site = json.loads((src / "site.json").read_text())
    pages = {}
    for path in (src / "pages").glob("*.json"):
        page = json.loads(path.read_text())
        pages[page["id"]] = page["variants"]

    def variant(name, locale):
        return pages[name].get(locale, pages[name][site["default"]])

    def lines(name, locale, depth=0):
        for block in variant(name, locale)["blocks"]:
            if "text" in block:
                yield block["text"]
            elif "link" in block:
                target = block["link"]
                yield f"[{variant(target, locale)['title']}]({target}.json)"
            else:
                child_locale = locale
                if depth and locale not in pages[name] and os.environ.get("ATLAS_TEST_LOSE_LOCALE") == "1":
                    child_locale = site["default"]
                yield from lines(block["include"], child_locale, depth + 1)

    for locale in site["locales"]:
        path = dest / "pages" / locale / "guide.json"
        path.parent.mkdir(parents=True)
        path.write_text(json.dumps(dict(id="guide", locale=locale, title=variant("guide", locale)["title"],
                                        lines=list(lines("guide", locale)))))
    print("{}")


if __name__ == "__main__":
    command, source, destination = sys.argv[1:]
    assert command == "build"
    build(Path(source), Path(destination))
