package org.dexsqltree.gui;

import javafx.application.Application;
import javafx.application.Platform;
import javafx.beans.property.ReadOnlyStringWrapper;
import javafx.collections.FXCollections;
import javafx.collections.ObservableList;
import javafx.concurrent.Task;
import javafx.geometry.Insets;
import javafx.geometry.Pos;
import javafx.scene.Scene;
import javafx.scene.control.Alert;
import javafx.scene.control.Button;
import javafx.scene.control.CheckBox;
import javafx.scene.control.ComboBox;
import javafx.scene.control.Label;
import javafx.scene.control.ListCell;
import javafx.scene.control.ListView;
import javafx.scene.control.ProgressIndicator;
import javafx.scene.control.Spinner;
import javafx.scene.control.SplitPane;
import javafx.scene.control.Tab;
import javafx.scene.control.TabPane;
import javafx.scene.control.TableCell;
import javafx.scene.control.TableColumn;
import javafx.scene.control.TableView;
import javafx.scene.control.TextArea;
import javafx.scene.control.TextField;
import javafx.scene.control.Tooltip;
import javafx.scene.control.TreeCell;
import javafx.scene.control.TreeItem;
import javafx.scene.control.TreeView;
import javafx.scene.input.KeyCode;
import javafx.scene.input.KeyCodeCombination;
import javafx.scene.input.KeyCombination;
import javafx.scene.layout.BorderPane;
import javafx.scene.layout.HBox;
import javafx.scene.layout.Priority;
import javafx.scene.layout.Region;
import javafx.scene.layout.StackPane;
import javafx.scene.layout.VBox;
import javafx.scene.shape.Circle;
import javafx.stage.FileChooser;
import javafx.stage.Stage;

import org.dexsqltree.client.CacheInfo;
import org.dexsqltree.client.CallNode;
import org.dexsqltree.client.CallTree;
import org.dexsqltree.client.DatasetInfo;
import org.dexsqltree.client.DexsqlClient;
import org.dexsqltree.client.DexsqlException;
import org.dexsqltree.client.DexsqlProcess;
import org.dexsqltree.client.Direction;
import org.dexsqltree.client.Health;
import org.dexsqltree.client.QueryResult;
import org.dexsqltree.client.TableInfo;

import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Optional;
import java.util.concurrent.Callable;
import java.util.concurrent.CancellationException;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.Consumer;

/**
 * JavaFX desktop shell for dexsql-tree.
 *
 * <p>The UI never parses DEX itself: the Go engine owns parsing and SQL. This
 * process only renders results, which keeps the desktop client small and
 * guarantees that the CLI, MCP server and GUI answer identically.
 *
 * <p>Every network call runs on {@link DexsqlClient#executor()}, a small daemon
 * pool shared by all tabs, so a slow query never freezes the window and several
 * panes can load at once. Only rendering happens on the FX thread, and result
 * tables are paged: a 5000-row answer costs one screen of cells, not 5000.
 */
public final class DexsqlTreeApp extends Application {
    private static final String DEFAULT_URL = "http://127.0.0.1:8765";
    private static final int MAX_ROWS = 5000;

    // Header
    private final TextField addressField = new TextField(DEFAULT_URL);
    private final Circle statusDot = new Circle(5);
    private final Label datasetLabel = new Label("No dataset");
    private final Button connectButton = new Button("Connect");
    private final Button openButton = new Button("Open APK");
    private final Button cacheButton = new Button("Cache");

    // Sidebar
    private final ListView<TableInfo> tableList = new ListView<>();
    private final TextArea schemaArea = new TextArea();

    // SQL tab
    private final TextArea sqlArea = new TextArea();
    private final TableView<ObservableList<String>> resultTable = new TableView<>();
    private final Button runButton = new Button("Run");
    private final Button cancelButton = new Button("Cancel");
    private final Label pageLabel = new Label();
    private final Button prevButton = new Button("Prev");
    private final Button nextButton = new Button("Next");
    private final ComboBox<Integer> pageSizeBox = new ComboBox<>();
    private final Label banner = new Label();

    // Search tab
    private final TextField keywordField = new TextField();
    private final CheckBox kindsClass = new CheckBox("classes");
    private final CheckBox kindsMethod = new CheckBox("methods");
    private final CheckBox kindsField = new CheckBox("fields");
    private final CheckBox kindsString = new CheckBox("strings");
    private final TableView<ObservableList<String>> searchTable = new TableView<>();
    private final Button searchButton = new Button("Search");

    // Tree tab
    private final TextField methodField = new TextField();
    private final ComboBox<Direction> directionBox = new ComboBox<>();
    private final Spinner<Integer> depthSpinner = new Spinner<>(1, 16, 3);
    private final TreeView<CallNode> treeView = new TreeView<>();
    private final TextArea smaliArea = new TextArea();
    private final Button treeButton = new Button("Build");

    // Status bar
    private final ProgressIndicator busy = new ProgressIndicator();
    private final Label statusLabel = new Label("Ready");
    private final Label engineLabel = new Label("engine");
    private final Label poolLabel = new Label();

    private TabPane tabs;
    private Tab sqlTab;
    private Tab searchTab;
    private Tab treeTab;
    private Stage stage;

    private DexsqlClient client;
    private DexsqlProcess process;
    private Health health;
    private String dataset;
    private List<TableInfo> allTables = List.of();

    /** Rows of the current answer, kept so paging never re-queries. */
    private QueryResult currentResult;
    private int page;
    private int pageSize = 200;

    private final AtomicInteger inFlight = new AtomicInteger();
    private Task<?> activeQuery;

    @Override
    public void start(Stage stage) {
        this.stage = stage;
        BorderPane root = new BorderPane();
        root.getStyleClass().add("root-pane");
        root.setTop(buildHeader());
        root.setLeft(buildSidebar());
        root.setCenter(buildCenter());
        root.setBottom(buildStatusBar());

        Scene scene = new Scene(root, 1320, 840);
        scene.getStylesheets().add(getClass().getResource("app.css").toExternalForm());
        installShortcuts(scene);

        stage.setTitle("dexsql-tree");
        stage.setScene(scene);
        stage.setMinWidth(1060);
        stage.setMinHeight(680);
        stage.show();

        handleArguments();
    }

    // ------------------------------------------------------------------ layout

    private Region buildHeader() {
        Label brand = new Label("dexsql-tree");
        brand.getStyleClass().add("brand");
        Label tagline = new Label("SQL and call trees for APK/DEX");
        tagline.getStyleClass().add("tagline");

        statusDot.getStyleClass().add("status-off");

        addressField.setPrefWidth(250);
        addressField.setOnAction(e -> connect());
        connectButton.getStyleClass().add("primary");
        connectButton.setOnAction(e -> connect());
        openButton.setOnAction(e -> chooseApk());
        openButton.setDisable(true);
        cacheButton.setOnAction(e -> showCache());
        cacheButton.setDisable(true);
        datasetLabel.getStyleClass().add("dataset-label");

        Region spacer = new Region();
        HBox.setHgrow(spacer, Priority.ALWAYS);
        HBox box = new HBox(10, brand, tagline, spacer, statusDot, addressField,
                connectButton, openButton, cacheButton, datasetLabel);
        box.setAlignment(Pos.CENTER_LEFT);
        box.getStyleClass().add("header");
        return box;
    }

    private Region buildSidebar() {
        Label title = new Label("Relations");
        title.getStyleClass().add("section-title");

        TextField filter = new TextField();
        filter.setPromptText("Filter tables");
        filter.textProperty().addListener((obs, old, text) -> applyTableFilter(text));

        tableList.setPlaceholder(new Label("Connect to browse"));
        tableList.setCellFactory(v -> new ListCell<>() {
            @Override
            protected void updateItem(TableInfo t, boolean empty) {
                super.updateItem(t, empty);
                if (empty || t == null) {
                    setText(null);
                    setTooltip(null);
                    return;
                }
                setText(t.isView() ? t.name() + "   view" : t.name());
                setTooltip(new Tooltip(t.doc() == null ? t.name() : t.doc()));
            }
        });
        tableList.getSelectionModel().selectedItemProperty().addListener((obs, old, t) -> {
            if (t != null) {
                showSchema(t);
            }
        });

        schemaArea.setEditable(false);
        schemaArea.setWrapText(true);
        schemaArea.getStyleClass().add("mono");
        schemaArea.setPrefRowCount(13);

        Label hint = new Label("Read-only queries. Ctrl+Enter runs, Ctrl+K searches, Ctrl+O opens a file.");
        hint.setWrapText(true);
        hint.getStyleClass().add("hint");

        Label schemaTitle = new Label("Schema");
        schemaTitle.getStyleClass().add("section-title");

        VBox box = new VBox(8, title, filter, tableList, schemaTitle, schemaArea, hint);
        VBox.setVgrow(tableList, Priority.ALWAYS);
        box.setPadding(new Insets(14));
        box.setPrefWidth(300);
        box.getStyleClass().add("sidebar");
        return box;
    }

    private Region buildCenter() {
        tabs = new TabPane();
        tabs.setTabClosingPolicy(TabPane.TabClosingPolicy.UNAVAILABLE);
        sqlTab = buildSqlTab();
        searchTab = buildSearchTab();
        treeTab = buildTreeTab();
        tabs.getTabs().addAll(sqlTab, searchTab, treeTab);
        return tabs;
    }

    private Tab buildSqlTab() {
        sqlArea.setWrapText(false);
        sqlArea.getStyleClass().add("mono");
        sqlArea.setText("SELECT descriptor, param_count, access_flags\n"
                + "FROM methods\n"
                + "ORDER BY descriptor\n"
                + "LIMIT 50;");
        sqlArea.setPrefRowCount(6);
        sqlArea.setOnKeyPressed(e -> {
            if (e.isControlDown() && e.getCode() == KeyCode.ENTER) {
                runQuery();
            }
        });

        runButton.getStyleClass().add("primary");
        runButton.setOnAction(e -> runQuery());
        cancelButton.getStyleClass().add("ghost");
        cancelButton.setDisable(true);
        cancelButton.setOnAction(e -> cancelQuery());
        Label hint = new Label("Ctrl+Enter");
        hint.getStyleClass().add("hint");

        pageSizeBox.getItems().setAll(FXCollections.observableArrayList(100, 200, 500, 1000));
        pageSizeBox.setValue(pageSize);
        pageSizeBox.setPrefWidth(90);
        pageSizeBox.setOnAction(e -> {
            pageSize = pageSizeBox.getValue();
            page = 0;
            renderPage();
        });
        prevButton.getStyleClass().add("ghost");
        nextButton.getStyleClass().add("ghost");
        prevButton.setDisable(true);
        nextButton.setDisable(true);
        prevButton.setOnAction(e -> {
            if (page > 0) {
                page--;
                renderPage();
            }
        });
        nextButton.setOnAction(e -> {
            if (currentResult != null && (page + 1) * pageSize < currentResult.rows().size()) {
                page++;
                renderPage();
            }
        });
        pageLabel.getStyleClass().add("hint");


        banner.setWrapText(true);
        banner.getStyleClass().add("banner");
        banner.setVisible(false);
        banner.setManaged(false);
        Button dismiss = new Button("Dismiss");
        dismiss.getStyleClass().add("ghost");
        dismiss.setOnAction(e -> hideBanner());
        HBox bannerBox = new HBox(8, banner, dismiss);
        bannerBox.setAlignment(Pos.CENTER_LEFT);
        bannerBox.visibleProperty().bind(banner.visibleProperty());
        bannerBox.managedProperty().bind(banner.managedProperty());

        resultTable.setPlaceholder(new Label("Run a query to see rows"));
        resultTable.getStyleClass().add("result-table");
        resultTable.setColumnResizePolicy(TableView.UNCONSTRAINED_RESIZE_POLICY);

        HBox bar = new HBox(8, runButton, cancelButton, hint, spacer(),
                prevButton, pageLabel, nextButton, new Label("rows"), pageSizeBox);
        bar.setAlignment(Pos.CENTER_LEFT);

        VBox box = new VBox(8, sqlArea, bar, bannerBox, resultTable);
        VBox.setVgrow(resultTable, Priority.ALWAYS);
        box.setPadding(new Insets(12));
        return new Tab("SQL Query", box);
    }

    private Tab buildSearchTab() {
        keywordField.setPromptText("Class, method, field or string fragment");
        keywordField.setPrefWidth(380);
        keywordField.setOnAction(e -> doSearch());
        searchButton.getStyleClass().add("primary");
        searchButton.setOnAction(e -> doSearch());

        for (CheckBox cb : List.of(kindsClass, kindsMethod, kindsField, kindsString)) {
            cb.setSelected(true);
            cb.getStyleClass().add("chip");
        }

        Label hint = new Label("Double-click a method to build its call tree; a class lists its methods.");
        hint.getStyleClass().add("hint");

        searchTable.setPlaceholder(new Label("Search across every symbol in the dataset"));
        searchTable.getStyleClass().add("result-table");
        searchTable.setColumnResizePolicy(TableView.UNCONSTRAINED_RESIZE_POLICY);
        searchTable.setOnMouseClicked(e -> {
            if (e.getClickCount() == 2) {
                openFromSearch();
            }
        });

        HBox bar = new HBox(8, new Label("Find"), keywordField, searchButton,
                kindsClass, kindsMethod, kindsField, kindsString, spacer());
        bar.setAlignment(Pos.CENTER_LEFT);
        VBox box = new VBox(8, bar, hint, searchTable);
        VBox.setVgrow(searchTable, Priority.ALWAYS);
        box.setPadding(new Insets(12));
        return new Tab("Search", box);
    }

    private Tab buildTreeTab() {
        methodField.setPromptText("Lcom/example/Foo;->bar(I)V");
        methodField.setPrefWidth(320);
        methodField.setMinWidth(200);
        methodField.setOnAction(e -> buildTree());
        directionBox.getItems().setAll(Direction.CALLERS, Direction.CALLEES);
        directionBox.getSelectionModel().select(Direction.CALLERS);
        directionBox.setPrefWidth(110);
        depthSpinner.setEditable(true);
        depthSpinner.setPrefWidth(86);
        treeButton.getStyleClass().add("primary");
        treeButton.setOnAction(e -> buildTree());

        Label methodLabel = fixedLabel("Method");
        Label depthLabel = fixedLabel("Depth");
        HBox bar = new HBox(8, methodLabel, methodField, directionBox,
                depthLabel, depthSpinner, treeButton, spacer());
        bar.setAlignment(Pos.CENTER_LEFT);

        treeView.setShowRoot(true);
        treeView.setCellFactory(v -> new CallNodeCell());
        smaliArea.setEditable(false);
        smaliArea.getStyleClass().add("mono");
        smaliArea.setPromptText("Enter a method descriptor above to build a caller or callee tree,\n"
                + "then double-click a node to disassemble it.");

        treeView.getSelectionModel().selectedItemProperty().addListener((obs, old, item) -> {
            if (item == null || item.getValue() == null) {
                return;
            }
            CallNode n = item.getValue();
            smaliArea.setText(n.descriptor()
                    + "\n\nfile_id=" + n.fileId() + "  method_idx=" + n.methodIdx()
                    + "  depth=" + n.depth() + "  calls=" + n.calls()
                    + (n.cycle() ? "  [cycle]" : ""));
        });
        treeView.setOnMouseClicked(e -> {
            if (e.getClickCount() == 2) {
                showSmali();
            }
        });

        Label treeEmpty = new Label("Enter a method descriptor to build a call tree");
        treeEmpty.getStyleClass().add("hint");
        treeEmpty.visibleProperty().bind(javafx.beans.binding.Bindings.isNull(treeView.rootProperty()));
        StackPane treeStack = new StackPane(treeView, treeEmpty);
        treeStack.setMinWidth(280);

        SplitPane split = new SplitPane(treeStack, smaliArea);
        split.setDividerPositions(0.55);
        VBox box = new VBox(8, bar, split);
        VBox.setVgrow(split, Priority.ALWAYS);
        box.setPadding(new Insets(12));
        return new Tab("Call Tree", box);
    }

    private Region buildStatusBar() {
        busy.setPrefSize(14, 14);
        busy.setVisible(false);
        busy.setManaged(false);
        engineLabel.getStyleClass().add("hint");
        poolLabel.getStyleClass().add("hint");
        Label remote = new Label(DEFAULT_URL);
        remote.getStyleClass().add("hint");
        remote.textProperty().bind(addressField.textProperty());

        Region spacer = new Region();
        HBox.setHgrow(spacer, Priority.ALWAYS);
        HBox bar = new HBox(10, busy, statusLabel, spacer, poolLabel, engineLabel, remote);
        bar.setAlignment(Pos.CENTER_LEFT);
        bar.getStyleClass().add("status-bar");
        return bar;
    }

    /** Labels that should keep their width instead of being clipped by an HBox. */
    private static Label fixedLabel(String text) {
        Label l = new Label(text);
        l.setMinWidth(Region.USE_PREF_SIZE);
        return l;
    }

    private static Region spacer() {
        Region r = new Region();
        HBox.setHgrow(r, Priority.ALWAYS);
        return r;
    }

    private void installShortcuts(Scene scene) {
        scene.getAccelerators().put(new KeyCodeCombination(KeyCode.O, KeyCombination.SHORTCUT_DOWN), this::chooseApk);
        scene.getAccelerators().put(new KeyCodeCombination(KeyCode.K, KeyCombination.SHORTCUT_DOWN), () -> {
            tabs.getSelectionModel().select(searchTab);
            keywordField.requestFocus();
        });
        scene.getAccelerators().put(new KeyCodeCombination(KeyCode.F5), this::connect);
    }

    // ------------------------------------------------------------------ state

    private void applyTableFilter(String text) {
        String needle = text == null ? "" : text.toLowerCase(Locale.ROOT);
        List<TableInfo> filtered = new ArrayList<>();
        for (TableInfo t : allTables) {
            if (needle.isEmpty() || t.name().toLowerCase(Locale.ROOT).contains(needle)) {
                filtered.add(t);
            }
        }
        tableList.setItems(FXCollections.observableArrayList(filtered));
    }

    private void showSchema(TableInfo t) {
        StringBuilder sb = new StringBuilder(t.name());
        if (t.kind() != null) {
            sb.append("   ").append(t.kind());
        }
        sb.append('\n');
        if (t.doc() != null) {
            sb.append(t.doc()).append("\n\n");
        }
        sb.append("Columns\n");
        for (String c : t.cols()) {
            sb.append("  ").append(c).append('\n');
        }
        schemaArea.setText(sb.toString());
    }

    // --------------------------------------------------------------- commands

    private void handleArguments() {
        List<String> args = getParameters().getRaw();
        String connectUrl = null;
        Path input = null;
        Path binary = null;
        for (int i = 0; i < args.size(); i++) {
            switch (args.get(i)) {
                case "--connect" -> connectUrl = i + 1 < args.size() ? args.get(++i) : null;
                case "--input" -> input = i + 1 < args.size() ? Path.of(args.get(++i)) : null;
                case "--bin" -> binary = i + 1 < args.size() ? Path.of(args.get(++i)) : null;
                default -> input = Path.of(args.get(i));
            }
        }
        if (connectUrl != null) {
            addressField.setText(connectUrl);
            connect();
            return;
        }
        if (input != null) {
            startLocal(input, binary);
            return;
        }
        connect();
    }

    private void startLocal(Path input, Path binary) {
        Path exe = binary;
        if (exe == null) {
            Optional<Path> found = DexsqlProcess.locateBinary();
            if (found.isEmpty()) {
                showBanner("Engine not found. Pass --bin or set DEXSQL_TREE_BIN.", true);
                return;
            }
            exe = found.get();
        }
        Path finalExe = exe;
        submit(() -> DexsqlProcess.start(finalExe, input), started -> {
            process = started;
            addressField.setText("http://127.0.0.1:" + process.port());
            connect();
        }, "Starting engine and parsing " + input.getFileName() + " ...");
    }

    private record Session(DexsqlClient client, Health health, List<TableInfo> tables, List<DatasetInfo> datasets) {
    }

    private void connect() {
        String url = addressField.getText().trim();
        if (url.isEmpty()) {
            return;
        }
        DexsqlClient candidate = new DexsqlClient(url);
        submit(() -> {
            Health h = candidate.healthInfo();
            List<TableInfo> tables = candidate.tables();
            List<DatasetInfo> datasets = candidate.datasets();
            return new Session(candidate, h, tables, datasets);
        }, session -> {
            client = session.client();
            health = session.health();
            allTables = session.tables();
            applyTableFilter("");
            if (!allTables.isEmpty()) {
                tableList.getSelectionModel().select(0);
            }
            if (!session.datasets().isEmpty()) {
                useDataset(session.datasets().get(0));
            } else {
                dataset = null;
                datasetLabel.setText("No dataset loaded");
            }
            openButton.setDisable(false);
            cacheButton.setDisable(false);
            setConnected(true);
            engineLabel.setText("engine " + health.version());
            poolLabel.setText(DexsqlClient.poolSize() + " worker threads");
            statusLabel.setText("Connected · " + plural(allTables.size(), "relation")
                    + " · profile " + health.profile());
        }, "Connecting to " + url + " ...");
    }

    private void useDataset(DatasetInfo d) {
        dataset = d.id();
        client.useDataset(d.id());
        datasetLabel.setText(shortName(d.path()) + "  ·  "
                + plural(d.stats().classes(), "class") + " / " + plural(d.stats().methods(), "method"));
    }

    private void chooseApk() {
        FileChooser fc = new FileChooser();
        fc.setTitle("Select APK or DEX");
        fc.getExtensionFilters().addAll(
                new FileChooser.ExtensionFilter("Android package", "*.apk", "*.dex", "*.jar", "*.zip"),
                new FileChooser.ExtensionFilter("All files", "*.*"));
        java.io.File f = fc.showOpenDialog(stage);
        if (f != null) {
            loadDataset(f.toPath());
        }
    }

    private void loadDataset(Path path) {
        if (client == null) {
            showBanner("Connect to an engine first.", true);
            return;
        }
        submit(() -> client.openDataset(path), d -> {
            useDataset(d);
            hideBanner();
            statusLabel.setText("Loaded " + shortName(d.path()) + " as " + d.id());
        }, "Parsing " + path.getFileName() + " ... first open builds the catalog");
    }

    private void runQuery() {
        if (client == null) {
            showBanner("Connect and load an APK first.", true);
            return;
        }
        String sql = sqlArea.getText().trim();
        if (sql.isEmpty()) {
            return;
        }
        String ds = dataset;
        Task<QueryResult> task = newSubmit(() -> client.query(sql, MAX_ROWS, ds), r -> {
            currentResult = r;
            page = 0;
            renderPage();
            hideBanner();
            statusLabel.setText(r.rowCount() + " rows · " + r.elapsedMs() + " ms"
                    + (r.truncated() ? " · truncated at " + MAX_ROWS : ""));
        }, "Running query ...");
        activeQuery = task;
        cancelButton.setDisable(false);
        task.runningProperty().addListener((obs, was, running) -> {
            if (!running) {
                activeQuery = null;
                cancelButton.setDisable(true);
            }
        });
    }

    private void cancelQuery() {
        if (activeQuery != null) {
            activeQuery.cancel(true);
            statusLabel.setText("Cancelling ...");
        }
    }

    private void doSearch() {
        if (client == null) {
            showBanner("Connect and load an APK first.", true);
            return;
        }
        String kw = keywordField.getText().trim();
        if (kw.isEmpty()) {
            return;
        }
        List<String> kinds = new ArrayList<>();
        if (kindsClass.isSelected()) {
            kinds.add("class");
        }
        if (kindsMethod.isSelected()) {
            kinds.add("method");
        }
        if (kindsField.isSelected()) {
            kinds.add("field");
        }
        if (kindsString.isSelected()) {
            kinds.add("string");
        }
        String ds = dataset;
        submit(() -> client.search(kw, kinds, 300, ds), r -> {
            renderInto(searchTable, r, 0, r.rows().size());
            hideBanner();
            statusLabel.setText(r.rowCount() + " hits · " + r.elapsedMs() + " ms");
        }, "Searching " + kw + " ...");
    }

    private void openFromSearch() {
        ObservableList<String> row = searchTable.getSelectionModel().getSelectedItem();
        if (row == null || row.size() < 4) {
            return;
        }
        String kind = row.get(0);
        String name = row.get(1);
        switch (kind) {
            case "method" -> {
                tabs.getSelectionModel().select(treeTab);
                methodField.setText(name);
                buildTree();
            }
            case "class" -> {
                sqlArea.setText("SELECT name, descriptor, access_flags, opcode_count\n"
                        + "FROM v_methods\nWHERE class_descriptor = '" + literal(name) + "'\n"
                        + "ORDER BY name;");
                tabs.getSelectionModel().select(sqlTab);
                runQuery();
            }
            case "field" -> {
                sqlArea.setText("SELECT * FROM fields WHERE descriptor = '" + literal(name) + "';");
                tabs.getSelectionModel().select(sqlTab);
                runQuery();
            }
            default -> {
                sqlArea.setText("SELECT method, value FROM method_strings\nWHERE value = '" + literal(name) + "';");
                tabs.getSelectionModel().select(sqlTab);
                runQuery();
            }
        }
    }

    private static String literal(String s) {
        return s.replace("'", "''");
    }

    private void buildTree() {
        if (client == null) {
            showBanner("Connect and load an APK first.", true);
            return;
        }
        String method = methodField.getText().trim();
        if (method.isEmpty()) {
            return;
        }
        Direction dir = directionBox.getValue();
        int depth = depthSpinner.getValue();
        String ds = dataset;
        submit(() -> client.tree(method, dir, depth, ds), tree -> {
            if (tree.root() != null) {
                TreeItem<CallNode> root = toItem(tree.root());
                root.setExpanded(true);
                treeView.setRoot(root);
            }
            hideBanner();
            statusLabel.setText(tree.nodeCount() + " nodes · " + tree.edgeCount() + " edges");
        }, "Building tree ...");
    }

    private TreeItem<CallNode> toItem(CallNode node) {
        TreeItem<CallNode> item = new TreeItem<>(node);
        if (node.children() != null) {
            for (CallNode c : node.children()) {
                item.getChildren().add(toItem(c));
            }
        }
        return item;
    }

    private void showSmali() {
        TreeItem<CallNode> item = treeView.getSelectionModel().getSelectedItem();
        if (item == null || item.getValue() == null || client == null) {
            return;
        }
        String desc = item.getValue().descriptor();
        String ds = dataset;
        submit(() -> client.smali(desc, ds), text -> {
            smaliArea.setText(text);
            hideBanner();
            statusLabel.setText("smali · " + desc);
        }, "Disassembling ...");
    }

    private void showCache() {
        if (client == null) {
            return;
        }
        submit(client::cache, info -> {
            Alert a = new Alert(Alert.AlertType.INFORMATION);
            a.initOwner(stage);
            a.setTitle("Catalog cache");
            a.setHeaderText(plural(info.count(), "catalog") + " · " + humanBytes(info.bytes()));

            StringBuilder sb = new StringBuilder(info.dir()).append("\n\n");
            sb.append(info.enabled() ? "new datasets are cached\n" : "caching is disabled\n");
            for (CacheInfo.Entry e : info.entries()) {
                sb.append(e.key()).append("   ").append(humanBytes(e.size())).append("   ").append(e.modTime()).append('\n');
            }
            TextArea area = new TextArea(sb.toString());
            area.setEditable(false);
            area.getStyleClass().add("mono");
            area.setPrefRowCount(Math.min(14, Math.max(4, info.entries().size() + 3)));
            area.setPrefColumnCount(72);

            Button clear = new Button("Clear cache");
            clear.getStyleClass().add("danger");
            clear.setOnAction(ev -> submit(client::clearCache, after -> {
                a.close();
                statusLabel.setText("Cache cleared · " + plural(after.count(), "catalog") + " left");
            }, "Clearing cache ..."));

            VBox content = new VBox(10, area, clear);
            a.getDialogPane().setContent(content);
            statusLabel.setText(info.count() == 0
                    ? "Cache is empty"
                    : plural(info.count(), "catalog") + " cached · " + humanBytes(info.bytes()));
            a.showAndWait();
        }, "Reading cache state ...");
    }

    // ---------------------------------------------------------------- render

    /** Rebuilds columns for one answer and adds rows [from, to). */
    private void renderInto(TableView<ObservableList<String>> table, QueryResult r, int from, int to) {
        table.getColumns().clear();
        table.getItems().clear();
        for (int i = 0; i < r.columns().size(); i++) {
            final int col = i;
            String name = r.columns().get(i);
            TableColumn<ObservableList<String>, String> c = new TableColumn<>(name);
            c.setCellValueFactory(data -> {
                String v = col < data.getValue().size() ? data.getValue().get(col) : null;
                return new ReadOnlyStringWrapper(v == null ? "NULL" : v);
            });
            c.setCellFactory(cv -> new TableCell<>() {
                @Override
                protected void updateItem(String item, boolean empty) {
                    super.updateItem(item, empty);
                    getStyleClass().remove("null-cell");
                    if (empty || item == null) {
                        setText(null);
                        return;
                    }
                    setText(item);
                    if ("NULL".equals(item)) {
                        getStyleClass().add("null-cell");
                    }
                }
            });
            // Header padding matters: with an unconstrained policy a column that
            // only fits its header text would ellipsise, hiding the column name.
            int width = Math.max(120, name.length() * 8 + 44);
            for (int row = from; row < Math.min(to, from + 12); row++) {
                Object v = r.cell(row, i);
                if (v != null) {
                    width = Math.max(width, Math.min(420, String.valueOf(v).length() * 8));
                }
            }
            c.setPrefWidth(Math.min(420, width));
            table.getColumns().add(c);
        }
        ObservableList<ObservableList<String>> lines = FXCollections.observableArrayList();
        for (int i = from; i < to; i++) {
            List<Object> row = r.rows().get(i);
            ObservableList<String> line = FXCollections.observableArrayList();
            for (Object v : row) {
                line.add(v == null ? null : String.valueOf(v));
            }
            lines.add(line);
        }
        table.setItems(lines);
    }

    /** Shows the current page of {@link #currentResult}. */
    private void renderPage() {
        if (currentResult == null) {
            pageLabel.setText("");
            prevButton.setDisable(true);
            nextButton.setDisable(true);
            return;
        }
        int total = currentResult.rows().size();
        int pages = Math.max(1, (total + pageSize - 1) / pageSize);
        if (page >= pages) {
            page = pages - 1;
        }
        int from = page * pageSize;
        int to = Math.min(total, from + pageSize);
        renderInto(resultTable, currentResult, from, to);
        pageLabel.setText("Page " + (page + 1) + " / " + pages + "   (" + total + " rows)");
        prevButton.setDisable(page == 0);
        nextButton.setDisable(page + 1 >= pages);
    }

    // ---------------------------------------------------------------- helpers

    private <T> void submit(Callable<T> work, Consumer<T> onSuccess, String busyText) {
        newSubmit(work, onSuccess, busyText);
    }

    private <T> Task<T> newSubmit(Callable<T> work, Consumer<T> onSuccess, String busyText) {
        beginBusy(busyText);
        Task<T> task = new Task<>() {
            @Override
            protected T call() throws Exception {
                return work.call();
            }
        };
        task.setOnSucceeded(e -> {
            endBusy();
            onSuccess.accept(task.getValue());
        });
        task.setOnCancelled(e -> {
            endBusy();
            statusLabel.setText("Cancelled");
        });
        task.setOnFailed(e -> {
            endBusy();
            Throwable err = task.getException();
            if (isCancellation(err)) {
                statusLabel.setText("Cancelled");
                return;
            }
            showBanner(rootMessage(err), true);
            statusLabel.setText("Failed");
        });
        DexsqlClient.executor().execute(task);
        return task;
    }

    private static boolean isCancellation(Throwable t) {
        Throwable cur = t;
        while (cur != null) {
            if (cur instanceof CancellationException || cur instanceof InterruptedException) {
                return true;
            }
            if (cur instanceof DexsqlException && cur.getMessage() != null
                    && cur.getMessage().toLowerCase(Locale.ROOT).contains("cancelled")) {
                return true;
            }
            cur = cur.getCause();
        }
        return false;
    }

    private void beginBusy(String text) {
        Platform.runLater(() -> {
            inFlight.incrementAndGet();
            busy.setVisible(true);
            busy.setManaged(true);
            statusLabel.setText(text);
            connectButton.setDisable(true);
        });
    }

    private void endBusy() {
        Platform.runLater(() -> {
            if (inFlight.decrementAndGet() <= 0) {
                inFlight.set(0);
                busy.setVisible(false);
                busy.setManaged(false);
                connectButton.setDisable(false);
                openButton.setDisable(client == null);
            }
        });
    }

    private void setConnected(boolean on) {
        statusDot.getStyleClass().setAll(on ? "status-on" : "status-off");
    }

    private void showBanner(String message, boolean error) {
        Platform.runLater(() -> {
            banner.setText(message);
            banner.getStyleClass().setAll("banner", error ? "banner-error" : "banner-info");
            banner.setVisible(true);
            banner.setManaged(true);
        });
    }

    private void hideBanner() {
        banner.setVisible(false);
        banner.setManaged(false);
    }

    private static String rootMessage(Throwable t) {
        Throwable cur = t;
        while (cur.getCause() != null) {
            cur = cur.getCause();
        }
        String msg = cur.getMessage() == null ? cur.toString() : cur.getMessage();
        // Framework exceptions can carry a page of diagnostics; the banner is
        // one line, so keep the first sentence-sized slice.
        if (msg.length() > 300) {
            msg = msg.substring(0, 300) + " ...";
        }
        return msg;
    }

    private static String plural(int n, String word) {
        return n + " " + word + (n == 1 ? "" : "s");
    }

    private static String shortName(String path) {
        if (path == null) {
            return "";
        }
        int i = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
        return i >= 0 ? path.substring(i + 1) : path;
    }

    private static String humanBytes(long n) {
        if (n < 1024) {
            return n + " B";
        }
        double kb = n / 1024.0;
        if (kb < 1024) {
            return String.format(Locale.ROOT, "%.1f KB", kb);
        }
        double mb = kb / 1024.0;
        if (mb < 1024) {
            return String.format(Locale.ROOT, "%.1f MB", mb);
        }
        return String.format(Locale.ROOT, "%.2f GB", mb / 1024.0);
    }

    @Override
    public void stop() {
        if (process != null) {
            process.close();
        }
    }

    /** Tree cell that shows the short name plus the owner class. */
    private static final class CallNodeCell extends TreeCell<CallNode> {
        @Override
        protected void updateItem(CallNode n, boolean empty) {
            super.updateItem(n, empty);
            getStyleClass().remove("cycle-cell");
            if (empty || n == null) {
                setText(null);
                setGraphic(null);
                return;
            }
            String suffix = n.calls() > 1 ? "  x" + n.calls() : "";
            if (n.depth() == 0) {
                setText(n.descriptor());
            } else {
                setText(n.name() + suffix + "   " + n.className());
            }
            if (n.cycle()) {
                getStyleClass().add("cycle-cell");
                setText(getText() + "   [cycle]");
            }
        }
    }
}
