package app.quoker.core.data.db

import androidx.room.Dao
import androidx.room.Database
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import androidx.room.RoomDatabase
import androidx.room.Transaction
import kotlinx.coroutines.flow.Flow

/** Offline cache: the user's targets and the first history page. Wiped on sign-out. */
@Database(
    entities = [TargetEntity::class, HistoryItemEntity::class, HistoryMetaEntity::class],
    version = 1,
    exportSchema = true,
)
abstract class QuokerDatabase : RoomDatabase() {
    abstract fun targetDao(): TargetDao

    abstract fun historyDao(): HistoryDao

    companion object {
        const val NAME = "quoker.db"
    }
}

/** A target as the core sent it; [position] keeps the server order (last used first, then title). */
@Entity(tableName = "targets")
data class TargetEntity(
    @PrimaryKey val id: Long,
    val position: Int,
    val title: String,
    val type: String,
    val status: String,
    val inactiveReason: String?,
    val hidden: Boolean,
    val lastUsedAt: String?,
)

/** A first-page history item; video and actions are stored as their wire JSON. */
@Entity(tableName = "history_items")
data class HistoryItemEntity(
    @PrimaryKey val id: Long,
    val position: Int,
    val videoJson: String,
    val actionsJson: String,
)

/** Single row (id = 0) describing the cached first page. */
@Entity(tableName = "history_meta")
data class HistoryMetaEntity(
    @PrimaryKey val id: Int = 0,
    val nextCursor: String?,
    val fetchedAtMillis: Long,
)

@Dao
abstract class TargetDao {
    @Query("SELECT * FROM targets ORDER BY position, id")
    abstract fun observeAll(): Flow<List<TargetEntity>>

    @Query("SELECT position FROM targets WHERE id = :id")
    abstract suspend fun positionOf(id: Long): Int?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract suspend fun upsert(targets: List<TargetEntity>)

    @Query("DELETE FROM targets")
    abstract suspend fun deleteAll()

    @Transaction
    open suspend fun replaceAll(targets: List<TargetEntity>) {
        deleteAll()
        upsert(targets)
    }
}

@Dao
abstract class HistoryDao {
    @Query("SELECT * FROM history_items ORDER BY position")
    abstract fun observeItems(): Flow<List<HistoryItemEntity>>

    @Query("SELECT * FROM history_meta WHERE id = 0")
    abstract fun observeMeta(): Flow<HistoryMetaEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    protected abstract suspend fun insertItems(items: List<HistoryItemEntity>)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    protected abstract suspend fun insertMeta(meta: HistoryMetaEntity)

    @Query("DELETE FROM history_items")
    protected abstract suspend fun deleteItems()

    @Query("DELETE FROM history_meta")
    protected abstract suspend fun deleteMeta()

    @Transaction
    open suspend fun replaceFirstPage(items: List<HistoryItemEntity>, meta: HistoryMetaEntity) {
        deleteItems()
        insertItems(items)
        insertMeta(meta)
    }

    @Transaction
    open suspend fun clear() {
        deleteItems()
        deleteMeta()
    }
}
