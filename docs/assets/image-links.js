// Wrap every content image in a target=_blank anchor pointing at the
// full-resolution PNG, so clicking opens the original in a new tab.
//
// Uses mkdocs-material's document$ observable so this re-runs on every
// instant-navigation page swap, not just initial load.
function wrapContentImages() {
  const imgs = document.querySelectorAll(".md-content img");
  imgs.forEach((img) => {
    if (img.closest("a")) return; // already linked
    const a = document.createElement("a");
    a.href = img.currentSrc || img.src;
    a.target = "_blank";
    a.rel = "noopener";
    a.title = "Open full-size in a new tab";
    img.parentNode.insertBefore(a, img);
    a.appendChild(img);
    img.style.cursor = "zoom-in";
  });
}

if (typeof document$ !== "undefined") {
  document$.subscribe(wrapContentImages);
} else {
  document.addEventListener("DOMContentLoaded", wrapContentImages);
}
