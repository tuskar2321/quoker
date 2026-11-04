package app.quoker.core.data.session

import android.content.Context
import com.google.crypto.tink.Aead
import com.google.crypto.tink.KeyTemplates
import com.google.crypto.tink.RegistryConfiguration
import com.google.crypto.tink.aead.AeadConfig
import com.google.crypto.tink.integration.android.AndroidKeysetManager
import java.security.GeneralSecurityException
import java.util.Base64

/** Encrypts the session token at rest. Behind an interface because Android Keystore is unavailable on the JVM. */
interface TokenCipher {
    fun encrypt(plaintext: String): String

    /** Null when the ciphertext cannot be decrypted (e.g. the Keystore key was lost on restore). */
    fun decrypt(ciphertext: String): String?
}

/** Tink AES-256-GCM; the keyset lives in SharedPreferences, wrapped by an Android Keystore master key. */
class TinkTokenCipher(private val context: Context) : TokenCipher {
    private val aead: Aead by lazy {
        AeadConfig.register()
        AndroidKeysetManager.Builder()
            .withSharedPref(context, KEYSET_NAME, PREF_FILE)
            .withKeyTemplate(KeyTemplates.get("AES256_GCM"))
            .withMasterKeyUri(MASTER_KEY_URI)
            .build()
            .keysetHandle
            .getPrimitive(RegistryConfiguration.get(), Aead::class.java)
    }

    override fun encrypt(plaintext: String): String =
        Base64.getEncoder().encodeToString(aead.encrypt(plaintext.toByteArray(Charsets.UTF_8), ASSOCIATED_DATA))

    override fun decrypt(ciphertext: String): String? = try {
        String(aead.decrypt(Base64.getDecoder().decode(ciphertext), ASSOCIATED_DATA), Charsets.UTF_8)
    } catch (_: GeneralSecurityException) {
        null
    } catch (_: IllegalArgumentException) {
        null
    }

    private companion object {
        const val KEYSET_NAME = "session_keyset"
        const val PREF_FILE = "quoker_tink"
        const val MASTER_KEY_URI = "android-keystore://quoker_session_master_key"
        val ASSOCIATED_DATA = "quoker.session_token".toByteArray(Charsets.UTF_8)
    }
}
