import pandas as pd
import matplotlib.pyplot as plt
import matplotlib
import glob
import os
import datetime as Datetime

font = {'family' : 'serif',
    'weight' : 'normal',
    'size'   : 17}

plt.rc('axes', titlesize=27)  # X and Y title size and weight
plt.rc('axes', labelsize=24)  # X and Y label size

matplotlib.rc('font', **font)

mig_slices = ["", "1g.5gb", "2g.10gb", "3g.20gb","4g.20gb","", "" , "7g.40gb"]
# 父資料夾路徑（相對路徑）
parent_folder = '.'

# 獲取所有子資料夾名稱
subfolders = [f.name for f in os.scandir(parent_folder) if f.is_dir()]

print(subfolders)

# 初始化圖表
plt.figure(figsize=(10, 6))

# 初始化一個空的字典來儲存每個子資料夾的名稱
subfolder_names = {}

for subfolder in subfolders:
    # 取得子資料夾中的所有 CSV 檔案
    if subfolder == '.venv':
        continue
    file_paths = glob.glob(os.path.join(parent_folder, subfolder, '*.csv'))
    
    for file_path in file_paths:
        # 讀取 CSV 檔案
        df = pd.read_csv(file_path)
        
        # 假設名稱在 CSV 檔案的第一列
        name = df.columns[1]
        
        # 將名稱儲存到字典中
        subfolder_names[file_path] = name.split('-deployment-')[0]

print(subfolder_names)

# 設定顏色
colors = plt.get_cmap('tab10', max(len(glob.glob(os.path.join(parent_folder, subfolder, '*.csv'))) for subfolder in subfolders))

for subfolder in subfolders:
    # 取得子資料夾中的所有 CSV 檔案
    file_paths = glob.glob(os.path.join(parent_folder, subfolder, '*.csv'))
    # 找到所有文件中的最小時間
    min_time = Datetime.datetime.now()
    file_paths.sort(key=lambda x: subfolder_names[x])
    for file_path in file_paths:
        df = pd.read_csv(file_path)
        df['Time'] = pd.to_datetime(df['Time'])
        min_time = min(min_time, df['Time'].min())
    
    for idx, file_path in enumerate(file_paths):
        # 讀取 CSV 檔案
        df = pd.read_csv(file_path)
        
        # 轉換時間格式
        df['Time'] = pd.to_datetime(df['Time'])
        
        # 計算時間差（以秒為單位）
        df['Time'] = (df['Time'] - min_time).dt.total_seconds()

        constant_value = int(df.columns[2])
        mig_slice = mig_slices[constant_value]
        
        # 繪製圖表
        #plt.ylim(0, 0.046)
       
        plt.gcf().set_size_inches(11, 8.8)
        plt.plot(df['Time'], df.iloc[:, 1], label=mig_slice, color=colors(idx), linewidth=4)
      

    # 設定圖表標題和標籤
    plt.title('Mean Time Per Token Duration')
    plt.xlabel('Time (seconds)')
    plt.ylabel('Mean Time Per Token Duration')
    plt.legend()
    plt.grid(True)

    plt.savefig(os.path.join(parent_folder, subfolder, f'{subfolder}.png'))

    # 顯示圖表
    plt.show()
    # 保存圖表

for subfolder in subfolders:
    # 取得子資料夾中的所有 CSV 檔案
    file_paths = glob.glob(os.path.join(parent_folder, subfolder, '*.csv'))
    # 找到所有文件中的最小時間
    min_time = Datetime.datetime.now()
    # 根據 subfolder_names[file_path] 的順序對 file_paths 進行排序
    file_paths.sort(key=lambda x: subfolder_names[x])
    for file_path in file_paths:
        df = pd.read_csv(file_path)
        df['Time'] = pd.to_datetime(df['Time'])
        min_time = min(min_time, df['Time'].min())
    
    for idx, file_path in enumerate(file_paths):
        # 讀取 CSV 檔案
        df = pd.read_csv(file_path)
        
        # 轉換時間格式
        df['Time'] = pd.to_datetime(df['Time'])
        
        # 計算時間差（以秒為單位）
        df['Time'] = (df['Time'] - min_time).dt.total_seconds()
        
        # 使用常數值
        constant_value = df.columns[2]
        mig_slice = mig_slices[int(constant_value)]
        
        # 繪製圖表
        plt.plot(df['Time'], [int(constant_value)] * len(df), label=mig_slice, color=colors(idx))
        plt.gca().yaxis.get_major_locator().set_params(integer=True)
        plt.ylim(0, 7)
        plt.fill_between(df['Time'], [int(constant_value)] * len(df), alpha=0.3, color=colors(idx))
        
        # 繪製線性遞減部分
        if idx != len(file_paths) - 1:
            end_time = df['Time'].max()
            linear_decrease_x = [end_time + i for i in range(1, 151)]
            linear_decrease_y = [int(constant_value) - (int(constant_value) / 150) * i for i in range(1, 151)]
            plt.plot(linear_decrease_x, linear_decrease_y, color=colors(idx), linestyle='--')
            plt.fill_between(linear_decrease_x, linear_decrease_y, alpha=0.3, color=colors(idx))

    # 設定圖表標題和標籤
    plt.title('GPU Resource')
    plt.xlabel('Time (seconds)')
    plt.ylabel('MIG usage')
    plt.legend()
    plt.grid(True)

    plt.gcf().set_size_inches(11, 8.8)
    plt.savefig(os.path.join(parent_folder, subfolder, f'{subfolder}_MIG.png'))

    # 顯示圖表
    plt.show()