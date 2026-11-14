const DEFAULT_LANG = 'zh-Hans';

const supportLangs = [
    {
        name: '汉语',
        value: 'zh-Hans',
        icon: '🇨🇳',
    },
    {
        name: 'English',
        value: 'en-US',
        icon: '🇺🇸',
    },
];

function getLang() {
    let lang = getCookie('lang');

    if (!isSupportLang(lang)) {
        lang = DEFAULT_LANG;
        setCookie('lang', lang);
    }

    return lang;
}

function setLang(lang) {
    if (!isSupportLang(lang)) {
        lang = DEFAULT_LANG;
    }

    setCookie('lang', lang);
    window.location.reload();
}

function isSupportLang(lang) {
    return supportLangs.some(l => l.value === lang);
}
