package app.quoker

import android.content.Context
import android.content.res.Configuration
import java.util.Locale

/**
 * The UI exists only in Russian (docs/08: Russian is the default resource language), so resources
 * resolve under the Russian configuration on any device locale. Otherwise plurals follow the
 * device's rules, e.g. English rules turn «7 дней» into «7 дня».
 */
internal object AppLocale {
    private val russian: Locale = Locale.forLanguageTag("ru")

    fun wrap(base: Context): Context {
        val config = Configuration(base.resources.configuration)
        if (config.locales[0]?.language == russian.language) return base
        config.setLocale(russian)
        return base.createConfigurationContext(config)
    }
}
