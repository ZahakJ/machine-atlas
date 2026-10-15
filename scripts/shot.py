#!/usr/bin/env python3
"""Take one screenshot of a page with headless Chromium.

Run it through scripts/shot.sh, which serializes calls with a lock. Uses
Playwright's installed Chromium, or /usr/bin/chromium when that is missing or
does not match; never downloads one.

Usage:
  scripts/shot.sh URL OUT.png [--width 1280] [--height 900] [--dark]
                  [--full-page] [--click SELECTOR]... [--wait-for SELECTOR]
                  [--eval JS] [--console]

--click is applied in order; --eval prints the JSON result of an expression
evaluated after the clicks; --console prints console messages and page errors
and makes the exit status 1 if any error occurred.
"""

import argparse
import json
import os
import sys

from playwright.sync_api import Error as PlaywrightError
from playwright.sync_api import sync_playwright

FLAGS = ["--disable-gpu", "--disable-dev-shm-usage", "--js-flags=--max-old-space-size=384"]
FALLBACK_CHROMIUM = "/usr/bin/chromium"
TIMEOUT_MS = 15000


def parse_args(argv):
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("url")
    p.add_argument("out", help="output PNG path")
    p.add_argument("--width", type=int, default=1280)
    p.add_argument("--height", type=int, default=900)
    p.add_argument("--dark", action="store_true", help="emulate prefers-color-scheme: dark")
    p.add_argument("--full-page", action="store_true")
    p.add_argument("--click", action="append", default=[], metavar="SELECTOR")
    p.add_argument("--wait-for", metavar="SELECTOR")
    p.add_argument("--eval", metavar="JS", dest="script")
    p.add_argument("--console", action="store_true")
    return p.parse_args(argv)


def launch(pw):
    """Playwright's own Chromium first; the system one if that is absent."""
    try:
        return pw.chromium.launch(headless=True, args=FLAGS)
    except PlaywrightError as err:
        if not os.path.exists(FALLBACK_CHROMIUM):
            raise
        print(f"shot: bundled Chromium unavailable ({str(err).splitlines()[0]}); using {FALLBACK_CHROMIUM}", file=sys.stderr)
        return pw.chromium.launch(headless=True, args=FLAGS, executable_path=FALLBACK_CHROMIUM)


def main(argv):
    args = parse_args(argv)
    messages, errors = [], []
    with sync_playwright() as pw:
        browser = launch(pw)
        try:
            page = browser.new_page(
                viewport={"width": args.width, "height": args.height},
                color_scheme="dark" if args.dark else "light",
                reduced_motion="reduce",
            )
            page.set_default_timeout(TIMEOUT_MS)

            def on_console(msg):
                messages.append(f"[{msg.type}] {msg.text}")
                if msg.type == "error":
                    errors.append(msg.text)

            def on_page_error(err):
                messages.append(f"[pageerror] {err}")
                errors.append(str(err))

            page.on("console", on_console)
            page.on("pageerror", on_page_error)
            page.goto(args.url, wait_until="load")
            if args.wait_for:
                page.wait_for_selector(args.wait_for)
            for selector in args.click:
                page.click(selector)
            if args.script:
                print(json.dumps(page.evaluate(args.script)))
            page.screenshot(path=args.out, full_page=args.full_page)
        finally:
            browser.close()
    if args.console:
        for line in messages:
            print(line)
        if errors:
            return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
