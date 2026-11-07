package main

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// Chrome glyphs are alpha masks tinted at use, so one asset serves both
// themes. scripts/render-icons.swift draws them from Apple's SF Symbols;
// this branch swaps in IconMCP's outlines, rendered by
// scripts/render-iconmcp.mjs from the definitions in icons/iconmcp.json.
//
//go:embed icons/*.png
var iconFiles embed.FS

// Agent marks are the vendors' filled logos, not stroke icons: the pane can
// then show which agent runs there instead of a generic terminal glyph.
type logoMark struct {
	viewBox string
	body    string
}

var logoMarks = map[string]logoMark{
	"claude": {"0 0 24 24", `<path d="m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2.55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607.8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2.4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091.255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828.2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1.3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.8396-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.6393-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.1275.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2.3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.3886-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.9446-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9064-1.3114Z"/>`},
	"codex":  {"0 0 24 24", `<path d="M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.073zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.8956zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z"/>`},
	"omp":    {"0 0 64 64", `<defs><linearGradient id="omp" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#ed4abf"/><stop offset=".5" stop-color="#9b4dff"/><stop offset="1" stop-color="#5ad8e6"/></linearGradient></defs><path fill="url(#omp)" d="M14 16h36v8H40v32h-8V24h-6v22h-8V24h-4z"/>`},
	"devin":  {"0 0 425 425", `<path d="M70 159.333V91.3471C70 88.3592 71.594 85.5983 74.1816 84.1044L133.043 50.1205C135.631 48.6265 138.819 48.6265 141.407 50.1205L200.269 84.1044C202.856 85.5983 204.45 88.3592 204.45 91.3471V126.068C204.708 137.606 210.806 148.734 221.531 154.926C232.256 161.117 244.942 160.834 255.063 155.289L285.132 137.929C287.719 136.435 290.907 136.435 293.495 137.929L352.357 171.913C354.944 173.406 356.538 176.167 356.538 179.155V247.123C356.538 250.111 354.944 252.872 352.357 254.366L293.495 288.35C290.907 289.844 287.719 289.844 285.132 288.35L255.306 271.13C245.146 265.456 232.344 265.117 221.534 271.358C210.809 277.55 204.711 288.678 204.453 300.215V334.926C204.453 337.914 202.859 340.675 200.271 342.169L141.41 376.153C138.822 377.647 135.634 377.647 133.046 376.153L74.1845 342.169C71.5969 340.675 70.0028 337.914 70.0028 334.926V266.959C70.0029 263.971 71.5969 261.21 74.1845 259.716L133.046 225.732C135.634 224.238 138.822 224.238 141.41 225.732L171.547 243.132C181.656 248.638 194.306 248.906 205.005 242.729C215.815 236.488 221.922 225.231 222.088 213.595C221.83 202.057 215.732 189.737 205.008 183.545C194.283 177.353 181.597 177.636 171.476 183.181L141.269 200.72C138.67 202.229 135.461 202.228 132.864 200.716L74.1576 166.562C71.5835 165.065 70 162.311 70 159.333Z"/>`},
	"grok":   {"0 0 512 512", `<path d="M210.484 312.759L343.465 210.383C349.984 205.364 359.302 207.322 362.408 215.117C378.758 256.231 371.454 305.64 338.925 339.563C306.397 373.487 261.137 380.927 219.768 363.983L174.577 385.803C239.394 432.008 318.104 420.581 367.289 369.251C406.303 328.564 418.386 273.104 407.088 223.091L407.19 223.198C390.807 149.726 411.218 120.359 453.03 60.3072C454.02 58.8833 455.01 57.4595 456 56L400.978 113.382V113.204L210.45 312.794"/><path d="M183.042 337.641C136.519 291.294 144.54 219.567 184.236 178.203C213.59 147.59 261.683 135.096 303.666 153.464L348.755 131.75C340.632 125.627 330.221 119.042 318.275 114.414C264.277 91.2407 199.63 102.774 155.735 148.516C113.513 192.549 100.236 260.254 123.036 318.027C140.069 361.206 112.148 391.748 84.0229 422.575C74.0561 433.503 64.0553 444.431 56 456L183.007 337.677"/>`},
	"droid":  {"0 0 508 508", `<path d="M321.997 150.712C321.401 150.568 320.844 150.299 320.363 149.925C319.883 149.551 319.491 149.08 319.215 148.544C318.938 148.008 318.783 147.42 318.76 146.821C318.738 146.22 318.848 145.624 319.084 145.07C327.226 125.716 330.819 110.23 325.021 103.747C309.666 86.5471 248.085 120.749 228.451 132.333C227.925 132.642 227.337 132.837 226.728 132.903C226.118 132.969 225.501 132.906 224.918 132.719C224.336 132.531 223.801 132.223 223.351 131.815C222.902 131.407 222.548 130.909 222.313 130.356C214.06 111.043 205.384 97.6094 196.589 97.0268C173.279 95.4688 154.491 162.187 148.991 183.932C148.844 184.515 148.57 185.06 148.188 185.528C147.805 185.998 147.323 186.381 146.775 186.651C146.227 186.921 145.626 187.072 145.012 187.094C144.399 187.116 143.788 187.009 143.221 186.778C123.406 178.825 107.545 175.316 100.914 180.98C83.305 195.978 118.315 256.126 130.175 275.304C130.492 275.816 130.692 276.391 130.76 276.987C130.829 277.582 130.765 278.186 130.573 278.755C130.381 279.325 130.065 279.847 129.647 280.286C129.228 280.725 128.718 281.07 128.15 281.298C108.384 289.359 94.6306 297.834 94.0272 306.424C92.439 329.192 160.74 347.544 183.01 352.916C183.605 353.061 184.16 353.33 184.64 353.704C185.118 354.077 185.509 354.548 185.785 355.083C186.061 355.618 186.215 356.205 186.237 356.803C186.26 357.402 186.151 357.998 185.916 358.551C177.773 377.905 174.181 393.398 179.979 399.874C195.334 417.074 256.921 382.877 276.556 371.293C277.081 370.984 277.67 370.789 278.28 370.722C278.889 370.655 279.507 370.717 280.09 370.905C280.673 371.093 281.207 371.402 281.657 371.81C282.106 372.219 282.46 372.717 282.694 373.271C290.947 392.578 299.616 406.012 308.417 406.601C331.728 408.153 350.516 341.44 356.009 319.688C356.157 319.106 356.432 318.562 356.816 318.094C357.2 317.625 357.682 317.243 358.231 316.974C358.779 316.705 359.381 316.554 359.995 316.533C360.608 316.511 361.219 316.619 361.786 316.85C381.601 324.803 397.455 328.304 404.093 322.648C421.702 307.65 386.684 247.495 374.825 228.317C374.51 227.804 374.312 227.229 374.245 226.634C374.177 226.039 374.242 225.436 374.434 224.868C374.626 224.299 374.941 223.777 375.358 223.338C375.775 222.899 376.284 222.552 376.85 222.323C396.623 214.261 410.376 205.786 410.973 197.196C412.568 174.428 344.26 156.078 321.997 150.712ZM295.254 128.885C299.734 136.73 276.646 189 259.474 225.561C259.186 226.172 258.715 226.682 258.121 227.024C257.528 227.365 256.842 227.521 256.155 227.47C255.468 227.419 254.814 227.164 254.28 226.739C253.746 226.314 253.358 225.739 253.169 225.093C246.234 201.322 238.306 173.392 229.824 149.683C229.491 148.752 229.508 147.736 229.871 146.817C230.235 145.897 230.921 145.133 231.808 144.662C252.989 133.363 289.234 118.358 295.254 128.885ZM193.746 135.355C202.589 137.807 224.103 190.714 238.424 228.426C238.664 229.056 238.699 229.742 238.527 230.393C238.354 231.044 237.983 231.627 237.461 232.065C236.939 232.503 236.292 232.775 235.608 232.844C234.923 232.913 234.234 232.775 233.632 232.45C211.501 220.453 185.694 206.159 162.529 195.253C161.622 194.823 160.901 194.093 160.493 193.192C160.085 192.292 160.018 191.279 160.303 190.335C167.12 167.736 181.865 132.069 193.746 135.355ZM126.652 210.04C134.676 205.664 188.197 228.216 225.621 244.989C226.248 245.269 226.771 245.73 227.12 246.31C227.47 246.889 227.629 247.56 227.577 248.23C227.524 248.901 227.264 249.54 226.828 250.062C226.393 250.582 225.805 250.962 225.143 251.147C200.813 257.921 172.211 265.664 147.937 273.949C146.985 274.272 145.946 274.255 145.007 273.9C144.067 273.545 143.286 272.876 142.805 272.011C131.257 251.322 115.867 215.92 126.652 210.04ZM133.275 309.188C135.779 300.551 189.952 279.537 228.562 265.548C229.207 265.315 229.91 265.28 230.576 265.448C231.243 265.617 231.84 265.98 232.288 266.49C232.736 266.999 233.015 267.631 233.085 268.299C233.155 268.968 233.015 269.641 232.682 270.23C220.392 291.846 205.758 317.053 194.592 339.672C194.156 340.561 193.409 341.269 192.486 341.668C191.563 342.068 190.525 342.134 189.557 341.853C166.42 335.235 129.905 320.792 133.275 309.188ZM209.739 374.722C205.252 366.884 228.347 314.608 245.519 278.054C245.806 277.442 246.279 276.931 246.872 276.59C247.465 276.249 248.151 276.093 248.838 276.144C249.525 276.194 250.179 276.45 250.713 276.875C251.247 277.3 251.634 277.874 251.824 278.521C258.759 302.285 266.686 330.222 275.169 353.932C275.499 354.862 275.481 355.877 275.117 356.795C274.752 357.713 274.064 358.475 273.178 358.945C252.004 370.223 215.752 385.256 209.76 374.722H209.739ZM311.247 368.252C302.397 365.807 280.883 312.894 266.562 275.182C266.322 274.55 266.285 273.862 266.458 273.21C266.63 272.559 267.003 271.974 267.526 271.536C268.049 271.097 268.697 270.826 269.382 270.758C270.068 270.69 270.759 270.83 271.361 271.157C293.485 283.154 319.299 297.455 342.457 308.362C343.366 308.789 344.089 309.519 344.497 310.42C344.905 311.321 344.971 312.335 344.683 313.28C337.872 335.912 323.128 371.544 311.247 368.252ZM378.341 293.566C370.31 297.949 316.795 275.391 279.365 258.618C278.738 258.338 278.215 257.877 277.866 257.297C277.516 256.718 277.357 256.047 277.409 255.377C277.461 254.706 277.722 254.067 278.158 253.546C278.593 253.025 279.181 252.646 279.843 252.461C304.18 245.687 332.775 237.943 357.049 229.658C358.003 229.335 359.043 229.353 359.984 229.709C360.925 230.065 361.706 230.737 362.188 231.603C373.729 252.285 389.119 287.693 378.341 293.566ZM371.718 194.419C369.207 203.063 315.041 224.077 276.431 238.066C275.784 238.3 275.08 238.335 274.413 238.167C273.746 237.999 273.148 237.635 272.698 237.124C272.249 236.613 271.972 235.98 271.903 235.31C271.833 234.641 271.975 233.966 272.311 233.377C284.594 211.768 299.228 186.554 310.394 163.935C310.833 163.048 311.58 162.343 312.502 161.945C313.425 161.546 314.462 161.481 315.429 161.76C338.566 168.413 375.081 182.815 371.718 194.419Z"/>`},
	"perch":  {"0 0 24 24", `<path fill-rule="evenodd" d="M4 3h9.9c3.4 0 6.1 2.7 6.1 6.1 0 3.4-2.7 6.1-6.1 6.1h-2.8v5.8H4V3zm5 4v4.2h4.9c1.2 0 2.1-.9 2.1-2.1s-.9-2.1-2.1-2.1H9z"/><path d="M16.2 8.6l5.3 1.2-5.3 1.6z"/>`},
	"pi":     {"0 0 800 800", `<path fill="#F09082" d="M165.29 165.29H517.36V400H400V282.65H165.29Z"/><path fill="#4D9ABF" d="M165.29 282.65H282.65V400H400V517.36H282.65V634.72H165.29Z"/><path fill="#F1BE58" d="M517.36 400H634.72V634.72H517.36Z"/>`},
}

// logoSource draws a brand mark as a filled path. Unlike iconSource it has
// no stroke, so the logo's own shape carries the weight.
func logoSource(name, color string) ui.ImageSource {
	key := "logo:" + name + "|" + color
	if v, ok := iconCache.Load(key); ok {
		return v.(ui.ImageSource)
	}
	mark, ok := logoMarks[name]
	if !ok {
		return nil
	}
	svg := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="%s" fill="%s">%s</svg>`,
		mark.viewBox, color, mark.body,
	)
	src, err := ui.DecodeImage([]byte(svg))
	if err != nil {
		return nil
	}
	iconCache.Store(key, src)
	return src
}

// claudeBrand is the coral of Claude's mark; it reads on both themes, so it
// does not follow the text colour the way a monochrome logo does.
const claudeBrand = "#d97757"

// perchBrand is the green of Perch's own mark.
const perchBrand = "#3f7a5a"

// agentLogoSource picks an agent's mark and its colour.
func agentLogoSource(kind agentKind, tk tokens) ui.ImageSource {
	switch kind {
	case agentClaude:
		return logoSource(string(kind), claudeBrand)
	case agentPerch:
		return logoSource(string(kind), perchBrand)
	}
	return logoSource(string(kind), colorHex(tk.Foreground))
}

var iconCache sync.Map // "name|color" -> ui.ImageSource

func iconSource(name, tint string) ui.ImageSource {
	key := name + "|" + tint
	if v, ok := iconCache.Load(key); ok {
		return v.(ui.ImageSource)
	}
	raw, err := iconFiles.ReadFile("icons/" + name + ".png")
	if err != nil {
		return nil
	}
	mask, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	r, g, b := hexColor(tint)
	bounds := mask.Bounds()
	tinted := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := mask.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			tinted.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: uint8(a >> 8)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, tinted); err != nil {
		return nil
	}
	src, err := ui.DecodeImage(buf.Bytes())
	if err != nil {
		return nil
	}
	iconCache.Store(key, src)
	return src
}

// hexColor parses the "#rrggbb" colour iconSource is given.
func hexColor(s string) (r, g, b uint8) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v)
}

func colorHex(c ui.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// glassUI mirrors the user's Liquid Glass choice for the chrome helpers,
// which build outside the workspace's methods. loadSettings and the switch
// in Settings keep it current.
var glassUI = true

// windowFocused is whether the main window has the keyboard. It is the app's
// own record rather than Context.WindowActive, because the main window is
// pinned as active (Window.SetActiveAppearance): the system would otherwise
// fade the native glass views out as the window loses the keyboard, and that
// fade, running under the solid frame the app draws in their place, is what
// made opening the settings window flicker. Pinned, the system fades nothing,
// and the app switches its own surfaces in one frame. Main thread only.
var windowFocused = true

// setWindowFocused records whether the main window has the keyboard, and has
// win draw again if that changed: its surfaces switch between glass and the
// theme's own colours with it.
func setWindowFocused(win *mygo.Window, on bool) {
	if windowFocused == on {
		return
	}
	windowFocused = on
	if win != nil {
		win.Invalidate()
	}
}

// liquid reports whether this window draws the glass look: the setting, a
// window the system gives a material to, and a window the user is working
// in. An inactive window's material is frosted by the system, which lifts
// every light tint into a grey sheet: the panes, the selected row and the
// search field turn milky the moment the settings window comes forward, or
// the user switches to another app. There the app draws its own surfaces
// instead, the colours it has with the glass turned off, so the window reads
// as the theme rather than as frost.
func liquid(c *ui.Context) bool { return glassUI && c.Vibrancy() && windowFocused }

// controlWash paints el with the theme's own ink: a hovered button, the
// search field or the selected row reads as the theme's wash on the window's
// glass. It is not a second glass view. A tinted glass there samples the
// already-frozen window and its regular style lightens whatever it sits on,
// so in light mode the wash vanishes into the glass and in dark mode it lifts
// into a flat, desaturated grey. wash is how much ink: a hover is 0.045, a
// selection 0.09. Where the window has no material, or the user turned the
// glass look off, fallback fills it as before.
func controlWash(c *ui.Context, el ui.Element, tk tokens, wash float32, fallback ui.Color) ui.Element {
	if !liquid(c) {
		return el.Background(fallback)
	}
	return el.Background(tk.Foreground.Alpha(wash))
}

// glassInFrame is the glass for a surface or control the frame paints, as a
// pane's card, its header's chips or the drawer: the shader's glass, which
// samples what the frame drew under it. The system's samples what is behind
// the window, which would cut a hole through the card rather than sit on it.
func glassInFrame(el ui.Element, tint ui.Color) ui.Element {
	return el.Material(glass.Glass{Tint: tint})
}

// glassChip is glassInFrame for a small control: the glass's own rim is lit
// on its top and bottom and shaded on its sides, so against a light surface
// the top and bottom edges disappear and the chip reads as cut. A hairline
// in the sides' shade gives all four edges the same weight.

// capsule is a control's own light surface, as a header button's hover or a
// chip: flat, with a hairline on all four sides, so it keeps one colour over
// the card's glass and matches the reference controls at this size.
func capsule(c *ui.Context, el ui.Element, tk tokens, fill, fallback ui.Color) ui.Element {
	if !glassUI {
		return el.Background(fallback)
	}
	return el.Background(fill).Border(1, tk.Foreground.Alpha(0.06))
}

// iconButton is a 26pt hit area with an SF Symbol and a quiet hover state,
// in place of mygo's default button chrome. A hint, when given, is the
// tooltip the pointer shows over it.
func iconButton(c *ui.Context, tk tokens, name string, action func(), hint ...string) {
	iconButtonSized(c, tk, name, iconSize, 17, iconOnMaterial, action, hint...)
}

// iconGlass is what a button's hover glass sits on.
type iconGlass uint8

const (
	// iconOnMaterial is the window's material, as a sidebar's.
	iconOnMaterial iconGlass = iota
	// iconOnCard is a pane card's own system glass.
	iconOnCard
	// iconInFrame is glass the frame paints, as the drawer's.
	iconInFrame
)

// iconButtonOnCard is iconButton over a pane card's system glass.
func iconButtonOnCard(c *ui.Context, tk tokens, name string, action func(), hint ...string) {
	iconButtonSized(c, tk, name, iconSize, 17, iconOnCard, action, hint...)
}

// iconButtonInFrame is iconButton over glass the frame paints, as the
// drawer's.
func iconButtonInFrame(c *ui.Context, tk tokens, name string, action func(), hint ...string) {
	iconButtonSized(c, tk, name, iconSize, 17, iconInFrame, action, hint...)
}

// iconButtonSmall is the list-row size: it sits quieter beside 12pt text.
// A hint, when given, is the tooltip the pointer shows over it.
func iconButtonSmall(c *ui.Context, tk tokens, name string, action func(), hint ...string) {
	iconButtonSized(c, tk, name, iconSmallSize, 15, iconOnMaterial, action, hint...)
}

// iconMenuButton is iconButton that opens a menu. The menu is built when it
// opens, so the host list is read fresh instead of at startup. A hint, when
// given, is the tooltip the pointer shows over it.
func iconMenuButton(c *ui.Context, tk tokens, name string, build func(m *ui.Menu), hint ...string) {
	size := iconSize
	el := ui.Row(c).Width(size).Height(size).Radius(chromeRadius).
		Center().Cursor(ui.CursorPointer)
	if el.Hovered() {
		el = controlWash(c, el, tk, 0.06, tk.Control)
	}
	if len(hint) > 0 && hint[0] != "" {
		el = el.Tooltip(hint[0])
	}
	el.Menu(build).Children(func() {
		if src := iconSource(name, colorHex(tk.MutedForeground)); src != nil {
			// The glyph sits inside the button: the press has to reach the
			// button so its menu opens.
			ui.Image(c, src).Width(17).Height(17).IgnoreInput(true)
		}
	})
}

func iconButtonSized(c *ui.Context, tk tokens, name string, size, glyph float32, glass iconGlass, action func(), hint ...string) {
	el := ui.Row(c).Width(size).Height(size).Radius(chromeRadius).
		Center().Cursor(ui.CursorPointer)
	if glass != iconOnMaterial {
		// The hairline the hover adds is reserved while it is away, so the
		// glyph does not move as the pointer comes and goes.
		el = el.Border(1, ui.Transparent)
	}
	if el.Hovered() {
		switch glass {
		case iconOnCard:
			el = capsule(c, el, tk, tk.Control.Alpha(0.3), tk.Control)
		case iconInFrame:
			el = capsule(c, el, tk, tk.Control.Alpha(0.3), tk.Control)
		default:
			el = controlWash(c, el, tk, 0.06, tk.Control)
		}
	}
	el = el.OnClick(action)
	if len(hint) > 0 && hint[0] != "" {
		el = el.Tooltip(hint[0])
	}
	el.Children(func() {
		if src := iconSource(name, colorHex(tk.MutedForeground)); src != nil {
			// The image is its own hit target: a click on it does not
			// reach the row behind it.
			ui.Image(c, src).Width(glyph).Height(glyph).OnClick(action)
		}
	})
}
