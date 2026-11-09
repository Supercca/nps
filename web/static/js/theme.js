/*
 * NPS 主题管理
 *
 * 职责：
 *   1. 读取 localStorage 中的主题偏好（light / dark），应用到 <html> 的 class 上。
 *   2. 提供切换按钮，切换后立即重绘 ECharts 图表。
 *   3. 页面在 <head> 中用一个内联脚本提前应用主题，避免首屏白闪；
 *      本脚本在 DOM 就绪后接管按钮交互与图表重绘。
 */
(function ($) {
    var STORAGE_KEY = 'nps_theme';
    var ROOT = document.documentElement;

    function readStored() {
        try {
            return window.localStorage.getItem(STORAGE_KEY);
        } catch (e) {
            return null;
        }
    }

    function store(theme) {
        try {
            window.localStorage.setItem(STORAGE_KEY, theme);
        } catch (e) {
            // 隐私模式下 localStorage 可能不可写，忽略即可（仅本次会话生效）
        }
    }

    // 未设置过偏好时跟随操作系统
    function systemPrefersDark() {
        return !!(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches);
    }

    function currentTheme() {
        var stored = readStored();
        if (stored === 'dark' || stored === 'light') {
            return stored;
        }
        return systemPrefersDark() ? 'dark' : 'light';
    }

    function isDark() {
        return ROOT.classList.contains('dark-theme');
    }

    // ECharts 在暗色背景下需要调整坐标轴 / 图例 / 提示框配色
    function applyChartTheme(dark) {
        if (typeof charts === 'undefined' || !charts) {
            return;
        }
        var axisLabel = dark ? '#8b949e' : '#666';
        var axisLine = dark ? '#30363d' : '#cccccc';
        var splitLine = dark ? 'rgba(255,255,255,0.08)' : '#eeeeee';
        var textColor = dark ? '#c9d1d9' : '#333333';

        for (var key in charts) {
            if (!charts.hasOwnProperty(key)) {
                continue;
            }
            var chart = charts[key];
            if (!chart || typeof chart.setOption !== 'function') {
                continue;
            }
            try {
                // 先读取当前配置，只给已存在的坐标轴着色，避免给饼图凭空加上坐标轴
                var current = chart.getOption();
                var payload = {
                    backgroundColor: 'transparent',
                    textStyle: { color: textColor },
                    legend: { textStyle: { color: textColor } },
                    tooltip: {
                        backgroundColor: dark ? 'rgba(33,38,45,0.95)' : '#ffffff',
                        borderColor: dark ? '#30363d' : '#cccccc',
                        textStyle: { color: textColor }
                    }
                };
                if (current && current.xAxis && current.xAxis.length) {
                    payload.xAxis = {
                        axisLabel: { color: axisLabel },
                        axisLine: { lineStyle: { color: axisLine } },
                        splitLine: { lineStyle: { color: splitLine } }
                    };
                }
                if (current && current.yAxis && current.yAxis.length) {
                    payload.yAxis = {
                        axisLabel: { color: axisLabel },
                        axisLine: { lineStyle: { color: axisLine } },
                        splitLine: { lineStyle: { color: splitLine } }
                    };
                }
                chart.setOption(payload);
            } catch (e) {
                // 单个图表重绘失败不影响其他图表
            }
        }
    }

    function syncButton(theme) {
        var $btn = $('#themeToggle');
        if ($btn.length === 0) {
            return;
        }
        var $icon = $btn.find('i');
        if ($icon.length > 0) {
            // 暗色下显示月亮（点击回到亮色），亮色下显示太阳
            $icon.attr('class', theme === 'dark' ? 'fa fa-moon' : 'fa fa-sun');
        }
        var $label = $btn.find('.theme-label');
        if ($label.length > 0) {
            // 语言资源异步加载，标签文本变化时同步到 title / aria-label
            var text = $.trim($label.text());
            if (text) {
                $btn.attr('title', text);
                $btn.attr('aria-label', text);
            }
        }
    }

    function apply(theme, persist) {
        if (theme === 'dark') {
            ROOT.classList.add('dark-theme');
        } else {
            ROOT.classList.remove('dark-theme');
        }
        if (persist) {
            store(theme);
        }
        syncButton(theme);
        applyChartTheme(theme === 'dark');
    }

    window.npsTheme = {
        current: currentTheme,
        isDark: isDark,
        apply: apply,
        applyChartTheme: function () {
            applyChartTheme(isDark());
        },
        toggle: function () {
            apply(isDark() ? 'light' : 'dark', true);
        }
    };

    $(function () {
        // 兜底：内联脚本已提前应用过，这里保证状态一致（如用户手动清过存储）
        apply(currentTheme(), false);

        $('#themeToggle').on('click', function (e) {
            e.preventDefault();
            window.npsTheme.toggle();
        });

        // i18n 文本异步填充，标签变化时同步按钮的 title / aria-label
        var $label = $('#themeToggle .theme-label');
        if ($label.length > 0 && window.MutationObserver) {
            var observer = new MutationObserver(function () {
                syncButton(currentTheme());
            });
            observer.observe($label[0], { childList: true, characterData: true, subtree: true });
        }
    });
})(jQuery);
