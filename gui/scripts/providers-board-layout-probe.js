/**
 * Providers fleet-board geometry, measured inside the dashboard page.
 *
 * providers-board-layout.test.ts evaluates this file as `(<file>)()` through the Chrome
 * DevTools Protocol and reads the returned receipt, so the numbers come from the real
 * built dashboard rather than from a model of it. Nothing here ships in the dashboard
 * bundle.
 */
async function providersBoardLayoutProbe() {
  const READY_DEADLINE_MS = 25_000;
  const SETTLE_MS = 250;

  const boxOf = (element) => {
    const rect = element.getBoundingClientRect();
    return {
      top: rect.top,
      bottom: rect.bottom,
      left: rect.left,
      right: rect.right,
      width: rect.width,
      height: rect.height,
    };
  };

  const pixels = (value) => {
    const parsed = Number.parseFloat(String(value));
    return Number.isFinite(parsed) ? parsed : 0;
  };

  const classNameOf = (element) => (typeof element.className === "string" ? element.className : element.tagName);

  /**
   * The lowest point a section paints at. Descendants an intervening scroll container
   * already clips do not count: that content scrolls inside its own port instead of
   * reaching a neighbour.
   */
  const contentBottomOf = (section, fallback) => {
    const escapes = (node) => {
      let current = node.parentElement;
      while (current && current !== section) {
        if (getComputedStyle(current).overflowY !== "visible") return false;
        current = current.parentElement;
      }
      return true;
    };
    let bottom = fallback;
    let tail = "";
    for (const node of section.querySelectorAll("*")) {
      const rect = node.getBoundingClientRect();
      if (rect.height <= 0 && rect.width <= 0) continue;
      if (!escapes(node)) continue;
      if (rect.bottom > bottom) {
        bottom = rect.bottom;
        tail = classNameOf(node);
      }
    }
    return { bottom, tail };
  };

  const describeSection = (section) => {
    const computed = getComputedStyle(section);
    const box = boxOf(section);
    const content = contentBottomOf(section, box.bottom);
    const slot = section.querySelector(".providers-overview-slot");
    const heading = section.querySelector("h4");
    return {
      label: section.getAttribute("aria-label"),
      heading: heading ? heading.textContent.trim() : null,
      box,
      clientHeight: section.clientHeight,
      scrollHeight: section.scrollHeight,
      minHeight: computed.minHeight,
      flexGrow: computed.flexGrow,
      flexShrink: computed.flexShrink,
      flexBasis: computed.flexBasis,
      overflowY: computed.overflowY,
      contentBottom: content.bottom,
      contentTail: content.tail,
      slot: slot
        ? {
          box: boxOf(slot),
          minHeight: pixels(getComputedStyle(slot).minHeight),
          clientHeight: slot.clientHeight,
          scrollHeight: slot.scrollHeight,
          overflowY: getComputedStyle(slot).overflowY,
        }
        : null,
    };
  };

  const startedAt = Date.now();
  while (document.querySelectorAll(".providers-overview-board > .providers-block").length !== 4) {
    if (Date.now() - startedAt > READY_DEADLINE_MS) {
      throw new Error("the Providers fleet board did not render four sections in time");
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  await document.fonts.ready;
  await new Promise((resolve) => setTimeout(resolve, SETTLE_MS));

  const board = document.querySelector(".providers-overview-board");
  const boardStyle = getComputedStyle(board);
  const root = document.documentElement;
  return {
    viewport: {
      width: window.innerWidth,
      height: window.innerHeight,
      devicePixelRatio: window.devicePixelRatio,
    },
    board: {
      box: boxOf(board),
      clientHeight: board.clientHeight,
      scrollHeight: board.scrollHeight,
      scrollTop: board.scrollTop,
      overflowY: boardStyle.overflowY,
      minHeight: boardStyle.minHeight,
    },
    document: {
      scrollWidth: root.scrollWidth,
      clientWidth: root.clientWidth,
      scrollHeight: root.scrollHeight,
      clientHeight: root.clientHeight,
      horizontalOverflow: root.scrollWidth > root.clientWidth,
    },
    sections: [...board.querySelectorAll(":scope > .providers-block")].map(describeSection),
  };
}

